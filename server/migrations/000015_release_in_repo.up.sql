-- 软件源提供的版本改为由上传时的「同步到软件源」开关决定：源里提供已同步且未撤回的最新版本。
ALTER TABLE releases ADD COLUMN in_repo TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否同步到软件源',
  ADD KEY idx_releases_repo (in_repo, status);
-- 沿用之前的规则：已推给全部设备的版本视为已同步到源，升级后源里的版本不变。
UPDATE releases SET in_repo = 1 WHERE id IN (
  SELECT release_id FROM (SELECT DISTINCT release_id FROM rollouts WHERE target = 'all' AND status IN ('active', 'completed')) t);
