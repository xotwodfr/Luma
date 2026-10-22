-- 0002_groups: group membership indexes and the one-owner invariant.
-- rollback (manual, only if no group has been created since):
--   DROP INDEX members_conv_active_idx; DROP INDEX members_one_owner_idx;
--   ALTER TABLE messages DROP CONSTRAINT messages_system_has_event;

-- Fan-out and member listing read active members of one conversation.
CREATE INDEX members_conv_active_idx ON members (conversation_id) WHERE left_at IS NULL;

-- At most one active owner per conversation, enforced by the database, not just the code.
-- (At least one while the group has members is kept by Leave's ownership transfer.)
CREATE UNIQUE INDEX members_one_owner_idx ON members (conversation_id) WHERE role = 'owner' AND left_at IS NULL;

-- System messages always say what happened: member_added, member_removed, member_left,
-- renamed, role_changed, owner_transferred.
ALTER TABLE messages ADD CONSTRAINT messages_system_has_event
    CHECK (kind <> 'system' OR meta->>'event' IN
        ('member_added', 'member_removed', 'member_left', 'renamed', 'role_changed', 'owner_transferred'));
