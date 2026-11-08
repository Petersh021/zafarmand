-- Reverse only migration 000011. All three tables are children of admin_users;
-- reverse creation order keeps the rollback dependency order explicit.
DROP TABLE public.admin_oidc_identities;
DROP TABLE public.admin_email_otp_challenges;
DROP TABLE public.admin_email_otp_rate_limits;
