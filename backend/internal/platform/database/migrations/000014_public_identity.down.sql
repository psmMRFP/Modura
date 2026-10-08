-- owner: identity
-- Refuse rollback while consumer accounts exist; deleting them implicitly is unsafe.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM modura.users WHERE consumer) THEN
        RAISE EXCEPTION 'remove consumer accounts through an explicit lifecycle workflow before rollback';
    END IF;
END $$;
DROP TABLE modura.public_identity_limits;
DROP TABLE modura.identity_mail_queue;
DROP TABLE modura.public_identity_events;
ALTER TABLE modura.auth_one_time_tokens DROP CONSTRAINT auth_one_time_tokens_scope_unique;
DROP TABLE modura.community_identity;
ALTER TABLE modura.auth_one_time_tokens DROP COLUMN bound_email;
ALTER TABLE modura.auth_one_time_tokens DROP COLUMN bound_security_version;
DELETE FROM modura.auth_one_time_tokens WHERE purpose = 'email_verification';
ALTER TABLE modura.auth_one_time_tokens DROP CONSTRAINT auth_one_time_tokens_purpose_check;
ALTER TABLE modura.auth_one_time_tokens ADD CONSTRAINT auth_one_time_tokens_purpose_check CHECK (purpose IN ('invitation', 'password_reset'));
ALTER TABLE modura.users DROP CONSTRAINT consumer_verified_active;
ALTER TABLE modura.users DROP COLUMN consumer;
ALTER TABLE modura.users DROP CONSTRAINT users_status_check;
ALTER TABLE modura.users ADD CONSTRAINT users_status_check CHECK (status IN ('invited', 'active', 'disabled', 'locked'));
