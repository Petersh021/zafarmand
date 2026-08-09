-- Reverse only the Stage 15 authentication boundary. Sessions must be removed
-- before their referenced users. Strict drops make unexpected dependencies fail
-- visibly instead of deleting schema owned by a later migration.
DROP TABLE public.admin_sessions;
DROP TABLE public.admin_users;
