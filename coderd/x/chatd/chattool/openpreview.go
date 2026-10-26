package chattool

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/fantasy"
	"github.com/google/uuid"

	"github.com/coder/coder/v2/coderd/workspaceapps/appurl"
	"github.com/coder/coder/v2/codersdk/workspacesdk"
)

const maxPreviewTitleLength = 80

// OpenPreviewResult is the open_preview tool result. The chat preview catalog
// decodes stored results with this type, so its JSON keys must stay stable.
type OpenPreviewResult struct {
	OK          bool      `json:"ok"`
	Title       string    `json:"title"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	AgentName   string    `json:"agent_name"`
	// Exactly one of Port or PreviewID identifies the preview on the agent.
	Port      int32  `json:"port,omitempty"`
	PreviewID string `json:"preview_id,omitempty"`
	// Path is the published workspace file or directory. It lets the user ask
	// the agent to republish the files after the daemon restarts and drops the
	// publication.
	Path string `json:"path,omitempty"`
}

// PreviewWorkspace binds a preview to the workspace connection used to open it.
type PreviewWorkspace struct {
	Conn        workspacesdk.AgentConn
	WorkspaceID uuid.UUID
	AgentName   string
}

// OpenPreviewOptions configures the open_preview tool.
type OpenPreviewOptions struct {
	GetWorkspace func(context.Context) (PreviewWorkspace, error)
}

// OpenPreviewArgs names a preview and selects either a running server or
// workspace files to show.
type OpenPreviewArgs struct {
	Title string `json:"title" description:"Short name for the preview shown in the user's preview selector, such as Docs site or Signup form. At most 80 characters."`
	Port  *int   `json:"port,omitempty" description:"Port of an existing HTTP server, between 1000 and 65535. Do not combine with path."`
	Path  string `json:"path,omitempty" description:"Absolute workspace path to an HTML file or a directory containing index.html. Do not combine with port."`
}

// OpenPreview returns a tool that opens a workspace preview in the user's UI.
func OpenPreview(options OpenPreviewOptions) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"open_preview",
		"Open and select an embedded preview for the user. Pass a short title and exactly one of port or path. "+
			"Use port for an existing HTTP web app; this does not start the app or wait for it to be ready. "+
			"Use path for an HTML file or a directory containing index.html; the workspace daemon publishes the files through Coder. "+
			"A file publishes only that file. To include CSS, JavaScript, images, or other pages, publish their directory instead. "+
			"Directory contents are published, so use a dedicated output directory without secrets. Directory listing is disabled. "+
			"Call this again to refresh after changing static files, or to restore a preview after its server or the workspace daemon restarts. "+
			"Path previews last until the workspace daemon stops, and at most 64 paths can be published until it restarts.",
		func(ctx context.Context, args OpenPreviewArgs, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			args.Title = strings.TrimSpace(args.Title)
			if args.Title == "" {
				return fantasy.NewTextErrorResponse("title is required"), nil
			}
			if utf8.RuneCountInString(args.Title) > maxPreviewTitleLength {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("title must be at most %d characters", maxPreviewTitleLength)), nil
			}
			// Format characters include invisible and bidirectional controls
			// that could disguise one preview's label as another's.
			hidden := func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }
			if strings.ContainsFunc(args.Title, hidden) {
				return fantasy.NewTextErrorResponse("title must not contain control or invisible formatting characters such as newlines, tabs, or bidirectional marks"), nil
			}

			args.Path = strings.TrimSpace(args.Path)
			hasPort, hasPath := args.Port != nil, args.Path != ""
			if hasPort == hasPath {
				return fantasy.NewTextErrorResponse("provide exactly one of port or path"), nil
			}

			// Numeric app hostnames recognize only four- or five-digit ports.
			if hasPort && (*args.Port < 1000 || *args.Port > 65535) {
				return fantasy.NewTextErrorResponse("port must be between 1000 and 65535"), nil
			}

			if options.GetWorkspace == nil {
				return fantasy.NewTextErrorResponse("workspace connection resolver is not configured"), nil
			}
			workspace, err := options.GetWorkspace(ctx)
			if err != nil {
				return workspaceLoadErrorResponse(err), nil
			}

			result := OpenPreviewResult{
				OK:          true,
				Title:       args.Title,
				WorkspaceID: workspace.WorkspaceID,
				AgentName:   workspace.AgentName,
			}
			if hasPort {
				result.Port = int32(*args.Port) //nolint:gosec // Range checked above.
				return marshalToolResponse(result), nil
			}

			preview, err := workspace.Conn.OpenPreview(ctx, workspacesdk.OpenPreviewRequest{Path: args.Path})
			if err != nil {
				return fantasy.NewTextErrorResponse("open file preview: " + err.Error()), nil
			}
			if !appurl.IsPreviewID(preview.ID) {
				return fantasy.NewTextErrorResponse("workspace daemon returned an invalid preview ID"), nil
			}

			result.PreviewID = preview.ID
			result.Path = preview.Path
			return marshalToolResponse(result), nil
		},
	)
}
