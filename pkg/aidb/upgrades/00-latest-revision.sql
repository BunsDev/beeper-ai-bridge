-- v0 -> v1: Initialize AI bridge session storage
CREATE TABLE ai_session (
	id TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	cwd TEXT NOT NULL,
	path TEXT NOT NULL,
	parent_session_path TEXT,
	leaf_id TEXT
);

CREATE TABLE ai_session_entry (
	session_id TEXT NOT NULL,
	id TEXT NOT NULL,
	parent_id TEXT,
	type TEXT NOT NULL,
	timestamp TEXT NOT NULL,
	data TEXT NOT NULL,
	PRIMARY KEY (session_id, id),
	FOREIGN KEY (session_id) REFERENCES ai_session(id) ON DELETE CASCADE
);

CREATE INDEX ai_session_entry_session_parent_idx ON ai_session_entry(session_id, parent_id);
CREATE INDEX ai_session_entry_session_type_idx ON ai_session_entry(session_id, type);
