-- Add metadata_user_id column to usage_logs for tracking per-external-user usage.
-- Stores the raw metadata.user_id value from Anthropic API request bodies.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS metadata_user_id VARCHAR(512);

-- Create index for metadata_user_id queries
CREATE INDEX IF NOT EXISTS idx_usage_logs_metadata_user_id ON usage_logs(metadata_user_id);
