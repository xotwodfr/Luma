-- 0004_member_title: a former member keeps the group title as it was when they left.
-- rollback (manual): ALTER TABLE members DROP COLUMN title_at_leave;
ALTER TABLE members ADD COLUMN title_at_leave text;
