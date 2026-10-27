CREATE TYPE workspace_build_deadline_reason AS ENUM (
	'build',
	'activity',
	'user_changed',
	'autostop_disabled',
	'template_schedule'
);

ALTER TABLE workspace_builds
	ADD COLUMN deadline_reason workspace_build_deadline_reason,
	ADD COLUMN deadline_changed_at timestamp with time zone NOT NULL DEFAULT '0001-01-01 00:00:00+00',
	ADD COLUMN deadline_details jsonb NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN workspace_builds.deadline_reason IS 'Why deadline last changed. NULL for builds whose deadline predates this column.';
COMMENT ON COLUMN workspace_builds.deadline_changed_at IS 'When deadline last changed.';
COMMENT ON COLUMN workspace_builds.deadline_details IS 'Reason-specific details of the last deadline change, for example {"sources": [...]} for activity.';

DROP VIEW workspace_build_with_user;

CREATE VIEW workspace_build_with_user AS
 SELECT workspace_builds.id,
    workspace_builds.created_at,
    workspace_builds.updated_at,
    workspace_builds.workspace_id,
    workspace_builds.template_version_id,
    workspace_builds.build_number,
    workspace_builds.transition,
    workspace_builds.initiator_id,
    workspace_builds.job_id,
    workspace_builds.deadline,
    workspace_builds.reason,
    workspace_builds.daily_cost,
    workspace_builds.max_deadline,
    workspace_builds.template_version_preset_id,
    workspace_builds.has_external_agent,
    workspace_builds.notified_autostop_deadline,
    workspace_builds.deadline_reason,
    workspace_builds.deadline_changed_at,
    workspace_builds.deadline_details,
    COALESCE(visible_users.avatar_url, ''::text) AS initiator_by_avatar_url,
    COALESCE(visible_users.username, ''::text) AS initiator_by_username,
    COALESCE(visible_users.name, ''::text) AS initiator_by_name
   FROM (workspace_builds
     LEFT JOIN visible_users ON ((workspace_builds.initiator_id = visible_users.id)));

COMMENT ON VIEW workspace_build_with_user IS 'Joins in the username + avatar url of the initiated by user.';
