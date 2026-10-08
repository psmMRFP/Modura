-- owner: feedback
-- Restricted global staff intake. Consumer-owned and anonymous submissions are
-- separate future entry points; this table must never be exposed publicly.
CREATE TABLE wheretolive.feedback_categories (
    key text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{0,49}$'),
    label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 100),
    active boolean NOT NULL DEFAULT true,
    position integer NOT NULL CHECK (position >= 0)
);
INSERT INTO wheretolive.feedback_categories (key,label,position) VALUES
('correction','信息纠错',0),('missing_place','缺少地点',1),('missing_visa','缺少签证信息',2),
('translation','翻译问题',3),('bug','功能故障',4),('source','来源问题',5),('suggestion','功能建议',6);

CREATE TABLE wheretolive.feedback_intake (
    id uuid PRIMARY KEY CHECK (substring(id::text from 15 for 1) = '7'),
    category_key text NOT NULL REFERENCES wheretolive.feedback_categories(key) ON DELETE RESTRICT,
    place_id uuid REFERENCES wheretolive.places(id) ON DELETE RESTRICT,
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    message text NOT NULL CHECK (char_length(btrim(message)) BETWEEN 1 AND 5000),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_review','resolved','dismissed')),
    outcome text CHECK (char_length(btrim(outcome)) BETWEEN 1 AND 2000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK ((status IN ('resolved','dismissed')) = (outcome IS NOT NULL))
);
CREATE INDEX feedback_intake_order ON wheretolive.feedback_intake(created_at DESC,id DESC);
CREATE INDEX feedback_intake_status ON wheretolive.feedback_intake(status,created_at DESC,id DESC);
