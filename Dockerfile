# The CI build places a statically linked Linux executable and the runner's
# runner-provided CA bundle in dist/. Keeping compilation outside this file
# leaves no compiler in the result and makes the selected CPU architecture an
# explicit responsibility of the release pipeline.
FROM scratch

# Root owns the immutable payload; the numeric runtime identity can read and
# execute it without being able to replace application files. Numeric USER
# avoids depending on /etc/passwd in scratch.
COPY dist/zafarmand /app/zafarmand
COPY dist/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY templates /app/templates
COPY static /app/static

WORKDIR /app
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
USER 65532:65532

EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/zafarmand"]
