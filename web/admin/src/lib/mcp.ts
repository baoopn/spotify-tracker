// Types and setup snippets for the MCP settings page (/admin/api/mcp).

export type McpTool = { name: string; description: string };

/** The dashboard-generated admin token (the token itself is never returned again). */
export type McpToken = {
	configured: boolean;
	prefix: string;
	created_at: string | null;
	last_used_at: string | null;
};

export type McpSettings = {
	/** MCP_ENABLED: false means /mcp answers 404. */
	enabled: boolean;
	endpoint_url: string;
	admin_tools_enabled: boolean;
	token: McpToken | null;
	/** MCP_ADMIN_TOKEN is set (it works as an admin token too). */
	env_token_configured: boolean;
	/** 0 = no limit. */
	rate_limit_per_minute: number;
	public_tools: McpTool[];
	admin_tools: McpTool[];
};

/** POST /mcp/token: the only response that carries the token. */
export type McpTokenCreated = { token: string; prefix: string; created_at: string };

export type McpSettingsUpdate = { admin_tools_enabled: boolean };

/** Stands in for the token in the snippets until one was just generated. */
export const TOKEN_PLACEHOLDER = '<token>';
/** The env var the project .mcp.json reads the token from. */
export const TOKEN_ENV = 'SPOTIFY_TRACKER_MCP_TOKEN';
const SERVER_NAME = 'spotify-tracker';

export function cliCommand(endpoint: string, token: string | null): string {
	return (
		`claude mcp add --transport http ${SERVER_NAME} ${endpoint} ` +
		`--header "Authorization: Bearer ${token ?? TOKEN_PLACEHOLDER}"`
	);
}

/** Project `.mcp.json`; an unset env var means public tools only. */
export function mcpJson(endpoint: string): string {
	const config = {
		mcpServers: {
			[SERVER_NAME]: {
				type: 'http',
				url: endpoint,
				headers: { Authorization: `Bearer \${${TOKEN_ENV}:-}` }
			}
		}
	};
	return JSON.stringify(config, null, 2);
}

export function setxCommand(token: string | null): string {
	return `setx ${TOKEN_ENV} "${token ?? TOKEN_PLACEHOLDER}"`;
}
