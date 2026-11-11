-- +goose Up
-- Tables and unique keys from the plan. content_hash is indexed and is not unique.
-- A job's identity lives on job_source_references, not on the job row.

CREATE TABLE sources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    type text NOT NULL,
    enabled boolean NOT NULL,
    configuration jsonb NOT NULL,
    cursor text NOT NULL DEFAULT '',
    last_checked_at timestamptz,
    last_success_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sources_name_key UNIQUE (name),
    CONSTRAINT sources_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT sources_type_not_blank CHECK (length(btrim(type)) > 0)
);

COMMENT ON TABLE sources IS 'Configured inputs. Secrets stay in the environment; configuration stores only an env var name.';

CREATE TABLE raw_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id uuid NOT NULL REFERENCES sources (id),
    provider_message_id text NOT NULL,
    received_at timestamptz NOT NULL,
    parse_status text NOT NULL,
    parse_error text,
    raw_body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT raw_messages_source_message_key UNIQUE (source_id, provider_message_id),
    CONSTRAINT raw_messages_provider_id_not_blank CHECK (length(btrim(provider_message_id)) > 0)
);

COMMENT ON TABLE raw_messages IS 'One ingested email so a parser change can replay it. Do not log raw_body.';

CREATE TABLE jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL,
    completeness text NOT NULL,
    title text NOT NULL,
    company text,
    author text,
    locations jsonb NOT NULL,
    description text NOT NULL DEFAULT '',
    language text,
    url text NOT NULL,
    apply_method text NOT NULL,
    employment_type text NOT NULL DEFAULT '',
    experience_level text NOT NULL DEFAULT '',
    deadline timestamptz,
    posted_at timestamptz,
    discovered_at timestamptz NOT NULL,
    content_hash text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT jobs_kind_check CHECK (kind IN ('listing', 'post')),
    CONSTRAINT jobs_completeness_check CHECK (completeness IN ('preview', 'full')),
    CONSTRAINT jobs_apply_method_check CHECK (apply_method IN ('url', 'contact_author', 'unknown')),
    CONSTRAINT jobs_status_check CHECK (status IN ('open', 'closed', 'unknown')),
    CONSTRAINT jobs_title_not_blank CHECK (length(btrim(title)) > 0),
    CONSTRAINT jobs_url_not_blank CHECK (length(btrim(url)) > 0)
);

CREATE INDEX jobs_company_idx ON jobs (company);
CREATE INDEX jobs_posted_at_idx ON jobs (posted_at);
CREATE INDEX jobs_content_hash_idx ON jobs (content_hash);
CREATE INDEX jobs_status_idx ON jobs (status);

CREATE TABLE job_source_references (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES jobs (id),
    source_id uuid NOT NULL REFERENCES sources (id),
    external_id text,
    original_url text NOT NULL,
    canonical_url text NOT NULL,
    discovered_at timestamptz NOT NULL,
    CONSTRAINT job_source_references_external_id_not_blank CHECK (external_id IS NULL OR length(btrim(external_id)) > 0),
    CONSTRAINT job_source_references_identity_present CHECK (
        external_id IS NOT NULL OR length(btrim(canonical_url)) > 0
    )
);

-- Present external id: one row per source and external id.
CREATE UNIQUE INDEX job_source_references_external_id_key
    ON job_source_references (source_id, external_id)
    WHERE external_id IS NOT NULL;

-- Absent external id: fall back to the canonical URL. A URL that also has an external id is not part of this key.
CREATE UNIQUE INDEX job_source_references_canonical_url_key
    ON job_source_references (source_id, canonical_url)
    WHERE external_id IS NULL;

CREATE TABLE candidate_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    profile_data jsonb NOT NULL,
    profile_version integer NOT NULL,
    active boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT candidate_profiles_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT candidate_profiles_version_check CHECK (profile_version > 0)
);

CREATE TABLE job_evaluations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES jobs (id),
    profile_id uuid NOT NULL REFERENCES candidate_profiles (id),
    profile_version integer NOT NULL,
    content_hash text NOT NULL,
    kind text NOT NULL,
    overall_score integer,
    skills_score integer,
    role_score integer,
    experience_score integer,
    location_score integer,
    company_score integer,
    evaluation_data jsonb,
    model text NOT NULL,
    prompt_version text NOT NULL,
    evaluated_at timestamptz NOT NULL,
    CONSTRAINT job_evaluations_kind_check CHECK (kind IN ('preview', 'evaluated')),
    CONSTRAINT job_evaluations_profile_version_check CHECK (profile_version > 0),
    CONSTRAINT job_evaluations_cache_key UNIQUE (job_id, profile_version, content_hash, prompt_version, model)
);

CREATE INDEX job_evaluations_job_id_idx ON job_evaluations (job_id);
CREATE INDEX job_evaluations_overall_score_idx ON job_evaluations (overall_score);

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES jobs (id),
    evaluation_id uuid REFERENCES job_evaluations (id),
    recipient text NOT NULL,
    notification_type text NOT NULL,
    channel text NOT NULL,
    status text NOT NULL,
    external_message_id text,
    sent_at timestamptz,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_recipient_not_blank CHECK (length(btrim(recipient)) > 0),
    CONSTRAINT notifications_type_check CHECK (notification_type IN ('preview', 'evaluated')),
    CONSTRAINT notifications_channel_check CHECK (channel IN ('telegram')),
    CONSTRAINT notifications_status_check CHECK (status IN ('pending', 'sent', 'failed')),
    CONSTRAINT notifications_job_recipient_type_key UNIQUE (job_id, recipient, notification_type)
);

CREATE INDEX notifications_job_id_idx ON notifications (job_id);

CREATE TABLE processing_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type text NOT NULL,
    job_id uuid REFERENCES jobs (id),
    source_id uuid REFERENCES sources (id),
    content_hash text,
    profile_version integer,
    status text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    locked_by text,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT processing_jobs_type_check CHECK (type IN ('match', 'poll')),
    CONSTRAINT processing_jobs_status_check CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    CONSTRAINT processing_jobs_attempts_check CHECK (attempts >= 0),
    CONSTRAINT processing_jobs_profile_version_check CHECK (profile_version IS NULL OR profile_version > 0),
    CONSTRAINT processing_jobs_shape_check CHECK (
        (
            type = 'match'
            AND job_id IS NOT NULL
            AND source_id IS NULL
            AND content_hash IS NOT NULL
            AND profile_version IS NOT NULL
        )
        OR (
            type = 'poll'
            AND source_id IS NOT NULL
            AND job_id IS NULL
            AND content_hash IS NULL
            AND profile_version IS NULL
        )
    )
);

-- One active match per job, content hash, and profile version. A completed row does not block a later run.
CREATE UNIQUE INDEX processing_jobs_active_match_key
    ON processing_jobs (type, job_id, content_hash, profile_version)
    WHERE status IN ('pending', 'processing')
      AND type = 'match';

-- One active poll per source.
CREATE UNIQUE INDEX processing_jobs_active_poll_key
    ON processing_jobs (type, source_id)
    WHERE status IN ('pending', 'processing')
      AND type = 'poll';

CREATE INDEX processing_jobs_status_available_at_idx ON processing_jobs (status, available_at);

CREATE TABLE source_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id uuid NOT NULL REFERENCES sources (id),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    status text NOT NULL,
    items_discovered integer NOT NULL DEFAULT 0,
    items_processed integer NOT NULL DEFAULT 0,
    error text,
    CONSTRAINT source_runs_counts_check CHECK (items_discovered >= 0 AND items_processed >= 0),
    CONSTRAINT source_runs_status_not_blank CHECK (length(btrim(status)) > 0)
);

CREATE TABLE user_feedback (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES jobs (id),
    profile_id uuid NOT NULL REFERENCES candidate_profiles (id),
    notification_id uuid NOT NULL REFERENCES notifications (id),
    feedback_type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_feedback_type_check CHECK (
        feedback_type IN ('interesting', 'not_relevant', 'saved_for_later', 'already_applied')
    )
);

-- +goose Down
DROP TABLE user_feedback;
DROP TABLE source_runs;
DROP TABLE processing_jobs;
DROP TABLE notifications;
DROP TABLE job_evaluations;
DROP TABLE candidate_profiles;
DROP TABLE job_source_references;
DROP TABLE jobs;
DROP TABLE raw_messages;
DROP TABLE sources;
