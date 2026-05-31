-- v0 -> v1: Latest AI bridge schema
CREATE TABLE ai_session (
	bridge_id TEXT NOT NULL,
	login_id TEXT NOT NULL,
	id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	parent_session_path TEXT,
	leaf_id TEXT,
	PRIMARY KEY (bridge_id, login_id, id),
	FOREIGN KEY (bridge_id, login_id) REFERENCES user_login(bridge_id, id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE ai_session_entry (
	bridge_id TEXT NOT NULL,
	login_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	id TEXT NOT NULL,
	parent_id TEXT,
	type TEXT NOT NULL,
	timestamp TEXT NOT NULL,
	data TEXT NOT NULL,
	PRIMARY KEY (bridge_id, login_id, session_id, id),
	FOREIGN KEY (bridge_id, login_id, session_id) REFERENCES ai_session(bridge_id, login_id, id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX ai_session_entry_login_session_parent_idx ON ai_session_entry(bridge_id, login_id, session_id, parent_id);
CREATE INDEX ai_session_entry_login_session_type_idx ON ai_session_entry(bridge_id, login_id, session_id, type);

CREATE TABLE ai_active_stream (
	bridge_id TEXT NOT NULL,
	run_id TEXT NOT NULL,
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
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (bridge_id, login_id, run_id),
	FOREIGN KEY (bridge_id, login_id) REFERENCES user_login(bridge_id, id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX ai_active_stream_login_updated_idx ON ai_active_stream(bridge_id, login_id, updated_at);
