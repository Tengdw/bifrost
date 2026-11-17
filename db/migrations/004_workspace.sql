CREATE TABLE bifrost.workspace_config (
 singleton boolean PRIMARY KEY CHECK(singleton), mode text NOT NULL CHECK(mode IN ('readonly','synthetic'))
);
