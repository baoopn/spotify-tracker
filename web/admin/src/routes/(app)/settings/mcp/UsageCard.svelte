<script lang="ts">
	import { TOKEN_ENV, cliCommand, mcpJson, setxCommand } from '#lib/mcp.js';
	import CodeSnippet from './CodeSnippet.svelte';

	let { endpoint, token }: { endpoint: string; token: string | null } = $props();

	const cli = $derived(cliCommand(endpoint, token));
	const cliPublic = $derived(`claude mcp add --transport http spotify-tracker ${endpoint}`);
	const json = $derived(mcpJson(endpoint));
	const setx = $derived(setxCommand(token));
</script>

<section class="card flex flex-col gap-5 p-6" aria-labelledby="mcp-usage-h">
	<div>
		<h2 id="mcp-usage-h" class="font-semibold text-fg">Use it from Claude</h2>
		<p class="mt-1 text-sm text-muted">
			{token
				? 'The snippets below contain the token you just generated.'
				: 'Replace <token> with your access token. Without a token you get the public tools only.'}
		</p>
	</div>

	<div class="flex flex-col gap-2">
		<h3 class="text-sm font-semibold text-fg">Claude Code (CLI)</h3>
		<p class="text-sm text-muted">With the admin tools:</p>
		<CodeSnippet code={cli} label="Copy Claude Code command" />
		<p class="text-sm text-muted">Public tools only:</p>
		<CodeSnippet code={cliPublic} label="Copy public Claude Code command" />
	</div>

	<div class="flex flex-col gap-2">
		<h3 class="text-sm font-semibold text-fg">Project .mcp.json</h3>
		<p class="text-sm text-muted">
			Reads the token from <code class="font-mono">{TOKEN_ENV}</code>; when it is unset the client
			gets the public tools.
		</p>
		<CodeSnippet code={json} label="Copy .mcp.json entry" />
		<p class="text-sm text-muted">Set the variable once (PowerShell), then restart Claude Code:</p>
		<CodeSnippet code={setx} label="Copy setx command" />
	</div>

	<div class="flex flex-col gap-2">
		<h3 class="text-sm font-semibold text-fg">claude.ai and Claude desktop</h3>
		<p class="text-sm text-muted">
			Add a custom connector with this URL. Connectors cannot send custom headers, so they get the
			public tools only.
		</p>
		<CodeSnippet code={endpoint} label="Copy connector URL" />
	</div>
</section>
