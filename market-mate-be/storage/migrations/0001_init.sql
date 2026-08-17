-- A video's transcript and the ingredient list extracted from it never change.
-- Both were previously discarded after fifteen minutes, so every repeat lookup
-- paid YouTube for a transcript that could not have moved and paid the model to
-- read it again. These two tables are the permanent record.

CREATE TABLE IF NOT EXISTS videos (
    video_id         TEXT PRIMARY KEY,
    title            TEXT        NOT NULL DEFAULT '',
    channel          TEXT        NOT NULL DEFAULT '',
    duration_seconds INTEGER     NOT NULL DEFAULT 0,
    transcript       TEXT        NOT NULL DEFAULT '',
    -- 'youtube' or 'fixture'. Provenance is part of the row's identity: a
    -- fixture transcript must never be read back to answer a live request.
    source           TEXT        NOT NULL,
    fetched_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS extractions (
    video_id      TEXT        NOT NULL REFERENCES videos (video_id) ON DELETE CASCADE,
    -- model name plus a fingerprint of the prompt that produced this list.
    -- Editing the prompt writes a new row rather than shadowing the old one, so
    -- a rollback still finds its own cached output.
    model_version TEXT        NOT NULL,
    ingredients   JSONB       NOT NULL,
    extracted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (video_id, model_version)
);

CREATE INDEX IF NOT EXISTS extractions_extracted_at_idx ON extractions (extracted_at DESC);

-- recipes is a view rather than a table: it is derived entirely from the two
-- tables above, and a table would need a trigger or a second write to stay
-- truthful. DISTINCT ON keeps the newest extraction per video, which is the one
-- the current prompt produced.
CREATE OR REPLACE VIEW recipes AS
SELECT DISTINCT ON (v.video_id)
       v.video_id,
       v.title,
       v.channel,
       v.duration_seconds,
       v.source,
       e.model_version,
       e.ingredients,
       e.extracted_at
FROM videos v
JOIN extractions e ON e.video_id = v.video_id
ORDER BY v.video_id, e.extracted_at DESC;
