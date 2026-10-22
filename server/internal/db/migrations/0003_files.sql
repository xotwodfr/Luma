-- 0003_files: attachment state for uploads.
-- rollback (manual): ALTER TABLE files DROP COLUMN attached_at, DROP COLUMN attached_seq;
--   ALTER TABLE messages DROP CONSTRAINT messages_file_has_id; DROP INDEX files_unattached_idx;

-- An upload is attached once, by its owner, to one message in the conversation it was
-- uploaded for. attached_seq is that message's seq: a member may fetch the file only when
-- attached_seq falls inside their visibility window, exactly like the message itself.
ALTER TABLE files
    ADD COLUMN attached_at  timestamptz,
    ADD COLUMN attached_seq bigint,
    ADD CONSTRAINT files_attached_together CHECK ((attached_at IS NULL) = (attached_seq IS NULL)),
    ADD CONSTRAINT files_attached_has_conversation CHECK (attached_at IS NULL OR conversation_id IS NOT NULL);

-- The orphan sweep finds never-attached uploads by age.
CREATE INDEX files_unattached_idx ON files (created_at) WHERE attached_at IS NULL;
-- The daily upload quota sums a user's recent uploads.
CREATE INDEX files_owner_created_idx ON files (owner_id, created_at);

ALTER TABLE messages ADD CONSTRAINT messages_file_has_id CHECK (kind <> 'file' OR meta ? 'file_id');
