-- v1 -> v2: Store active AI streams
CREATE TABLE ai_active_stream (
	run_id TEXT PRIMARY KEY,
	login_id TEXT NOT NULL,
	portal_id TEXT NOT NULL,
	portal_receiver TEXT NOT NULL DEFAULT '',
	room_id TEXT NOT NULL,
	event_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	model_id TEXT NOT NULL,
	entry_id TEXT NOT NULL DEFAULT '',
	run_json TEXT NOT NULL,
	metadata_json TEXT NOT NULL,
	status_info_json TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX ai_active_stream_login_updated_idx ON ai_active_stream(login_id, updated_at);
