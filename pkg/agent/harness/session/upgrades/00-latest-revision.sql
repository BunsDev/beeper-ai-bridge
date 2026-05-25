-- v0 -> v1: Initialize agent session storage
CREATE TABLE sessions (
	id text primary key,
	created_at text not null,
	path text not null,
	parent_session_path text,
	leaf_id text
);

CREATE TABLE entries (
	session_id text not null,
	id text not null,
	parent_id text,
	type text not null,
	timestamp text not null,
	data text not null,
	primary key (session_id, id)
);

CREATE INDEX entries_session_parent_idx ON entries(session_id, parent_id);
