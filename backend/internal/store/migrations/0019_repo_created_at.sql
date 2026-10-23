-- GitHub repository creation time breaks ties between manifests for one app.
ALTER TABLE repos ADD COLUMN created_at DATETIME;
