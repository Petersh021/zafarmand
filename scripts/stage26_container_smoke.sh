#!/usr/bin/env bash

# Exercise the exact scratch image boundary against a separately named,
# disposable PostgreSQL database. The script intentionally uses only reserved
# example.invalid identities and one inert local password.
set -Eeuo pipefail

readonly expected_confirmation="stage26-disposable-postgresql-database"
readonly smoke_database_name="zafarmand_stage26_smoke_test"
readonly smoke_admin_email="stage26-smoke-admin@example.invalid"
readonly smoke_admin_password="stage26-smoke-password-only"
readonly smoke_inquiry_email="stage26-smoke-inquiry@example.invalid"
readonly smoke_http_address="127.0.0.1:18080"
readonly smoke_base_url="http://${smoke_http_address}"
# Every image invocation proves that migrations, bootstrap, and serving need no
# writable container filesystem, Linux capability, or privilege escalation.
readonly -a docker_hardening=(
	--read-only
	--cap-drop ALL
	--security-opt no-new-privileges
)

: "${STAGE26_IMAGE:?STAGE26_IMAGE is required}"
: "${STAGE26_POSTGRES_ADMIN_URL:?STAGE26_POSTGRES_ADMIN_URL is required}"
: "${STAGE26_SMOKE_DATABASE_URL:?STAGE26_SMOKE_DATABASE_URL is required}"

if [[ "${STAGE26_SMOKE_CONFIRM:-}" != "${expected_confirmation}" ]]; then
	echo "Stage 26 smoke confirmation is missing or invalid." >&2
	exit 2
fi

for dependency in curl docker psql awk grep mktemp; do
	if ! command -v "${dependency}" >/dev/null 2>&1; then
		echo "Stage 26 smoke dependency is unavailable: ${dependency}" >&2
		exit 2
	fi
done

image_user="$(docker image inspect \
	--format '{{.Config.User}}' \
	"${STAGE26_IMAGE}")"
if [[ "${image_user}" != "65532:65532" ]]; then
	echo "Stage 26 deployment image does not use the reviewed non-root identity." >&2
	exit 2
fi

smoke_directory="$(mktemp -d)"
container_id=""
database_created="false"

# The cleanup keeps cancellation and assertion failures isolated from later CI
# runs. It prints container logs only on failure; successful logs remain quiet.
cleanup() {
	local status=$?
	local cleanup_failed="false"
	local stop_failed="false"
	local shutdown_exit_code=""
	local container_log="${smoke_directory}/container.log"

	trap - EXIT INT TERM

	if [[ -n "${container_id}" ]] && \
		docker container inspect "${container_id}" >/dev/null 2>&1; then
		# A successful docker stop alone is insufficient: Docker may return success
		# after its deadline and SIGKILL the process. Require both exit code zero and
		# the application's fixed completion event before claiming graceful shutdown.
		if ! docker container stop --time 10 "${container_id}" >/dev/null; then
			stop_failed="true"
			cleanup_failed="true"
		fi
		if ! docker container logs "${container_id}" >"${container_log}" 2>&1; then
			cleanup_failed="true"
		fi
		if [[ "${stop_failed}" == "false" ]]; then
			if ! shutdown_exit_code="$(docker container inspect \
				--format '{{.State.ExitCode}}' \
				"${container_id}")"; then
				cleanup_failed="true"
			elif [[ "${shutdown_exit_code}" != "0" ]]; then
				cleanup_failed="true"
			fi
			if ! grep --fixed-strings --quiet -- \
				'"msg":"server_shutdown_completed"' \
				"${container_log}"; then
				cleanup_failed="true"
			fi
		fi

		if (( status != 0 )) || [[ "${cleanup_failed}" == "true" ]]; then
			echo "Stage 26 container logs:" >&2
			if [[ -s "${container_log}" ]]; then
				cat "${container_log}" >&2
			fi
		fi
		if [[ "${stop_failed}" == "true" ]]; then
			docker container rm --force "${container_id}" >/dev/null || true
		elif ! docker container rm "${container_id}" >/dev/null; then
			cleanup_failed="true"
		fi
	fi

	if [[ "${database_created}" == "true" ]]; then
		# The SQL target is a fixed *_test identifier, never an environment value.
		if ! psql "${STAGE26_POSTGRES_ADMIN_URL}" \
			--no-psqlrc \
			--set ON_ERROR_STOP=1 \
			--command "DROP DATABASE IF EXISTS ${smoke_database_name} WITH (FORCE)" \
			>/dev/null; then
			cleanup_failed="true"
		fi
	fi

	rm -rf -- "${smoke_directory}"

	if (( status == 0 )) && [[ "${cleanup_failed}" == "true" ]]; then
		status=1
	fi
	exit "${status}"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	echo "Stage 26 smoke check failed: $1" >&2
	return 1
}

# extract_hidden_value follows the checked-in templates' multiline input
# layout without evaluating HTML or requiring an additional parser package.
extract_hidden_value() {
	local field_name=$1
	local document=$2

	awk -v field_name="${field_name}" '
		$0 ~ ("name=\"" field_name "\"") { in_field = 1 }
		in_field && match($0, /value="[^"]+"/) {
			print substr($0, RSTART + 7, RLENGTH - 8)
			exit
		}
	' "${document}"
}

assert_body_contains() {
	local document=$1
	local expected=$2

	if ! grep --fixed-strings --quiet -- "${expected}" "${document}"; then
		fail "response did not contain the expected application marker"
	fi
}

assert_redirect() {
	local headers=$1
	local expected=$2
	local location

	# curl preserves the HTTP carriage return in dumped headers. Remove only that
	# delimiter before comparing the server-owned relative redirect exactly.
	location="$(awk '
		tolower($1) == "location:" {
			sub(/\r$/, "", $2)
			print $2
			exit
		}
	' "${headers}")"
	if [[ "${location}" != "${expected}" ]]; then
		fail "redirect location was not ${expected}"
	fi
}

request_get_200() {
	local path=$1
	local document=$2
	local status

	status="$(curl \
		--silent \
		--show-error \
		--output "${document}" \
		--write-out '%{http_code}' \
		"${smoke_base_url}${path}")"
	if [[ "${status}" != "200" ]]; then
		fail "GET ${path} returned HTTP ${status}"
	fi
}

# Refuse to perform database lifecycle work through a connection that already
# targets the fixed smoke database; cleanup must always remain out-of-band.
admin_database="$(psql "${STAGE26_POSTGRES_ADMIN_URL}" \
	--no-psqlrc \
	--tuples-only \
	--no-align \
	--set ON_ERROR_STOP=1 \
	--command 'SELECT current_database()')"
if [[ "${admin_database}" == "${smoke_database_name}" ]]; then
	fail "the PostgreSQL administration URL targets the disposable database"
fi

psql "${STAGE26_POSTGRES_ADMIN_URL}" \
	--no-psqlrc \
	--set ON_ERROR_STOP=1 \
	--command "DROP DATABASE IF EXISTS ${smoke_database_name} WITH (FORCE)" \
	>/dev/null
database_created="true"
psql "${STAGE26_POSTGRES_ADMIN_URL}" \
	--no-psqlrc \
	--set ON_ERROR_STOP=1 \
	--command "CREATE DATABASE ${smoke_database_name}" \
	>/dev/null

# Verify the supplied application URL before the image can migrate it. This
# guard prevents a typo from directing the smoke binary at another database.
application_database="$(psql "${STAGE26_SMOKE_DATABASE_URL}" \
	--no-psqlrc \
	--tuples-only \
	--no-align \
	--set ON_ERROR_STOP=1 \
	--command 'SELECT current_database()')"
if [[ "${application_database}" != "${smoke_database_name}" ]]; then
	fail "the application URL does not target the disposable smoke database"
fi

# Host networking lets the non-root container retain the application's safe
# loopback-only HTTP default while reaching the job-owned PostgreSQL service.
(
	export DATABASE_URL="${STAGE26_SMOKE_DATABASE_URL}"
	export ZAFARMAND_REQUIRE_DATABASE_TLS="false"
	docker run \
		--rm \
		--network host \
		"${docker_hardening[@]}" \
		--env DATABASE_URL \
		--env ZAFARMAND_REQUIRE_DATABASE_TLS \
		"${STAGE26_IMAGE}" \
		migrate up
)

(
	export DATABASE_URL="${STAGE26_SMOKE_DATABASE_URL}"
	export ZAFARMAND_REQUIRE_DATABASE_TLS="false"
	export ZAFARMAND_ADMIN_PASSWORD="${smoke_admin_password}"
	docker run \
		--rm \
		--network host \
		"${docker_hardening[@]}" \
		--env DATABASE_URL \
		--env ZAFARMAND_REQUIRE_DATABASE_TLS \
		--env ZAFARMAND_ADMIN_PASSWORD \
		"${STAGE26_IMAGE}" \
		admin create-user \
		--email "${smoke_admin_email}" \
		--role owner
)

container_name="zafarmand-stage26-smoke-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}"
container_id="$(
	(
		export DATABASE_URL="${STAGE26_SMOKE_DATABASE_URL}"
		export ZAFARMAND_REQUIRE_DATABASE_TLS="false"
		export ZAFARMAND_HTTP_ADDRESS="${smoke_http_address}"
		export ZAFARMAND_EXTERNAL_HTTPS="false"
		docker run \
			--detach \
			--name "${container_name}" \
			--network host \
			"${docker_hardening[@]}" \
			--env DATABASE_URL \
			--env ZAFARMAND_REQUIRE_DATABASE_TLS \
			--env ZAFARMAND_HTTP_ADDRESS \
			--env ZAFARMAND_EXTERNAL_HTTPS \
			"${STAGE26_IMAGE}"
	)
)"

# Liveness is the first synchronization point; a separate exact request below
# still verifies its public response contract after startup.
for _ in {1..30}; do
	if curl --silent --fail "${smoke_base_url}/health/live" >/dev/null; then
		break
	fi
	if [[ "$(docker container inspect \
		--format '{{.State.Running}}' \
		"${container_id}" 2>/dev/null || true)" != "true" ]]; then
		fail "container exited before becoming live"
	fi
	sleep 1
done
if ! curl --silent --fail "${smoke_base_url}/health/live" >/dev/null; then
	fail "container did not become live within 30 seconds"
fi

request_get_200 "/health/live" "${smoke_directory}/live.txt"
if [[ "$(<"${smoke_directory}/live.txt")" != "live" ]]; then
	fail "liveness body was not exact"
fi

request_get_200 "/health/ready" "${smoke_directory}/ready.txt"
if [[ "$(<"${smoke_directory}/ready.txt")" != "ready" ]]; then
	fail "readiness body was not exact"
fi

request_get_200 "/" "${smoke_directory}/home.html"
assert_body_contains "${smoke_directory}/home.html" 'id="main-content"'
assert_body_contains "${smoke_directory}/home.html" 'Zafarmand'

request_get_200 "/products" "${smoke_directory}/products.html"
assert_body_contains "${smoke_directory}/products.html" 'Product catalogue'

request_get_200 "/interior-design" "${smoke_directory}/interior.html"
assert_body_contains "${smoke_directory}/interior.html" 'Interior project index'

request_get_200 "/architecture-design" "${smoke_directory}/architecture.html"
assert_body_contains "${smoke_directory}/architecture.html" 'Architecture project index'

request_get_200 "/static/css/main.css" "${smoke_directory}/main.css"
assert_body_contains "${smoke_directory}/main.css" ':root {'

cookie_jar="${smoke_directory}/cookies.txt"
login_page="${smoke_directory}/login.html"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie-jar "${cookie_jar}" \
	--output "${login_page}" \
	--write-out '%{http_code}' \
	"${smoke_base_url}/admin/login")"
if [[ "${request_status}" != "200" ]]; then
	fail "admin login page returned HTTP ${request_status}"
fi
login_csrf="$(extract_hidden_value csrf_token "${login_page}")"
if [[ -z "${login_csrf}" ]]; then
	fail "admin login CSRF token was not rendered"
fi

login_headers="${smoke_directory}/login-headers.txt"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie "${cookie_jar}" \
	--cookie-jar "${cookie_jar}" \
	--dump-header "${login_headers}" \
	--output "${smoke_directory}/login-response.txt" \
	--write-out '%{http_code}' \
	--data-urlencode "csrf_token=${login_csrf}" \
	--data-urlencode "email=${smoke_admin_email}" \
	--data-urlencode "password=${smoke_admin_password}" \
	"${smoke_base_url}/admin/login")"
if [[ "${request_status}" != "303" ]]; then
	fail "valid administrator login did not redirect to the dashboard"
fi
assert_redirect "${login_headers}" "/admin"

dashboard="${smoke_directory}/dashboard.html"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie "${cookie_jar}" \
	--cookie-jar "${cookie_jar}" \
	--output "${dashboard}" \
	--write-out '%{http_code}' \
	"${smoke_base_url}/admin")"
if [[ "${request_status}" != "200" ]]; then
	fail "authenticated dashboard returned HTTP ${request_status}"
fi
assert_body_contains "${dashboard}" 'Administration overview'
logout_csrf="$(extract_hidden_value csrf_token "${dashboard}")"
if [[ -z "${logout_csrf}" ]]; then
	fail "authenticated logout CSRF token was not rendered"
fi

# Preserving the authenticated cookie before logout lets the next request replay
# the revoked token. The ordinary jar below still accepts the server's deletion
# cookie so the browser-style logout path is exercised independently.
revoked_cookie_jar="${smoke_directory}/revoked-cookies.txt"
cp -- "${cookie_jar}" "${revoked_cookie_jar}"
logout_headers="${smoke_directory}/logout-headers.txt"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie "${cookie_jar}" \
	--cookie-jar "${cookie_jar}" \
	--dump-header "${logout_headers}" \
	--output "${smoke_directory}/logout-response.txt" \
	--write-out '%{http_code}' \
	--data-urlencode "csrf_token=${logout_csrf}" \
	"${smoke_base_url}/admin/logout")"
if [[ "${request_status}" != "303" ]]; then
	fail "administrator logout did not return to the login page"
fi
assert_redirect "${logout_headers}" "/admin/login"

# A post-logout dashboard request proves server-side revocation, not merely a
# browser cookie deletion: it deliberately replays the pre-logout session.
post_logout_headers="${smoke_directory}/post-logout-headers.txt"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie "${revoked_cookie_jar}" \
	--dump-header "${post_logout_headers}" \
	--output "${smoke_directory}/post-logout.txt" \
	--write-out '%{http_code}' \
	"${smoke_base_url}/admin")"
if [[ "${request_status}" != "303" ]]; then
	fail "post-logout dashboard access was not rejected"
fi
assert_redirect "${post_logout_headers}" "/admin/login"

contact_page="${smoke_directory}/contact.html"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie-jar "${cookie_jar}" \
	--output "${contact_page}" \
	--write-out '%{http_code}' \
	"${smoke_base_url}/contact")"
if [[ "${request_status}" != "200" ]]; then
	fail "Contact page returned HTTP ${request_status}"
fi
contact_csrf="$(extract_hidden_value csrf_token "${contact_page}")"
submission_token="$(extract_hidden_value submission_token "${contact_page}")"
if [[ -z "${contact_csrf}" || -z "${submission_token}" ]]; then
	fail "Contact security tokens were not rendered"
fi

contact_headers="${smoke_directory}/contact-headers.txt"
request_status="$(curl \
	--silent \
	--show-error \
	--cookie "${cookie_jar}" \
	--cookie-jar "${cookie_jar}" \
	--dump-header "${contact_headers}" \
	--output "${smoke_directory}/contact-response.txt" \
	--write-out '%{http_code}' \
	--data-urlencode "csrf_token=${contact_csrf}" \
	--data-urlencode "submission_token=${submission_token}" \
	--data-urlencode 'name=Stage 26 Smoke Test' \
	--data-urlencode "email=${smoke_inquiry_email}" \
	--data-urlencode 'discipline=architecture-design' \
	--data-urlencode 'message=Automated Stage 26 deployment smoke test; inert test data only.' \
	"${smoke_base_url}/contact")"
if [[ "${request_status}" != "303" ]]; then
	fail "valid Contact inquiry did not redirect to confirmation"
fi
assert_redirect "${contact_headers}" "/contact#contact-form-response"

confirmation="${smoke_directory}/confirmation.html"
request_status="$(curl \
	--silent \
	--show-error \
	--location \
	--cookie "${cookie_jar}" \
	--cookie-jar "${cookie_jar}" \
	--output "${confirmation}" \
	--write-out '%{http_code}' \
	"${smoke_base_url}/contact")"
if [[ "${request_status}" != "200" ]]; then
	fail "Contact confirmation returned HTTP ${request_status}"
fi
assert_body_contains "${confirmation}" 'Your inquiry has been saved for the studio to review.'

# Query only an aggregate over the reserved identity; no submitted value is
# printed into the CI log.
inquiry_count="$(psql "${STAGE26_SMOKE_DATABASE_URL}" \
	--no-psqlrc \
	--tuples-only \
	--no-align \
	--set ON_ERROR_STOP=1 \
	--command "SELECT count(*) FROM public.inquiries WHERE email = '${smoke_inquiry_email}'")"
if [[ "${inquiry_count}" != "1" ]]; then
	fail "Contact smoke submission was not persisted exactly once"
fi

echo "Stage 26 container smoke checks passed."
