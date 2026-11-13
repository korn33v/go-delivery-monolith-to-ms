CREATE TABLE consumer_failures (
 consumer_group text NOT NULL,
 source_topic text NOT NULL,
 source_partition integer NOT NULL,
 source_offset bigint NOT NULL,
 attempts integer NOT NULL CHECK(attempts>0),
 last_error text NOT NULL,
 status text NOT NULL CHECK(status IN ('RETRYING','RESOLVED','QUARANTINED')),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(consumer_group,source_topic,source_partition,source_offset)
);
CREATE TABLE dead_letters (
 id text PRIMARY KEY,
 consumer_group text NOT NULL,
 source_topic text NOT NULL,
 source_partition integer NOT NULL,
 source_offset bigint NOT NULL,
 envelope jsonb NOT NULL,
 quarantined_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(consumer_group,source_topic,source_partition,source_offset),
 FOREIGN KEY(consumer_group,source_topic,source_partition,source_offset)
 REFERENCES consumer_failures(consumer_group,source_topic,source_partition,source_offset)
);
