-- v2 -> v3: Scope AI bridge-owned rows to bridge logins
ALTER TABLE ai_session ADD COLUMN bridge_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_session ADD COLUMN login_id TEXT NOT NULL DEFAULT '';

ALTER TABLE ai_session_entry ADD COLUMN bridge_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_session_entry ADD COLUMN login_id TEXT NOT NULL DEFAULT '';

CREATE INDEX ai_session_login_idx ON ai_session(bridge_id, login_id);
CREATE INDEX ai_session_entry_login_session_parent_idx ON ai_session_entry(bridge_id, login_id, session_id, parent_id);
CREATE INDEX ai_session_entry_login_session_type_idx ON ai_session_entry(bridge_id, login_id, session_id, type);

ALTER TABLE ai_active_stream ADD COLUMN bridge_id TEXT NOT NULL DEFAULT '';

DROP INDEX ai_active_stream_login_updated_idx;
CREATE UNIQUE INDEX ai_active_stream_bridge_login_run_idx ON ai_active_stream(bridge_id, login_id, run_id);
CREATE INDEX ai_active_stream_login_updated_idx ON ai_active_stream(bridge_id, login_id, updated_at);
