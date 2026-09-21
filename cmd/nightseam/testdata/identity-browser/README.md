# Native-browser identity exchange

The normal full suite compiles `browser.ts` against this checkout's runtime.
The browser execution test is opt-in and requires an installed Chromium browser;
it does not download one, use Node's WebSocket, or add a browser driver dependency.

After `pnpm install --frozen-lockfile`, run from the repository root:

```powershell
$env:NIGHTSEAM_BROWSER = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
go test ./cmd/nightseam -run '^TestIdentityBrowserExchange$' -count=1 -v
Remove-Item Env:NIGHTSEAM_BROWSER
```

On Linux, set `NIGHTSEAM_BROWSER` to the installed Chromium executable instead.
The browser runs headless with a fresh temporary profile. The Go test serves the
compiled modules and eight real WebSocket endpoints on a loopback-only test
server, waits for the page's report, and stops the browser. No browser sandbox
flags are changed. The report includes the actual browser user agent.

Each of matching digest, differing digest, absent `identity.check`, and a peer
with no digest is exercised with no subprotocol and with the consumer's
`ticket.fixture` subprotocol. The page uses `new WebSocket` from its browser
global and the real TypeScript `checkIdentity` helper. The Go server independently
counts application invocations and records offered subprotocols. A mismatch must
produce the exact `contract_mismatch` error and make zero application calls;
the other cases make one call. The ordinary identity RPC never becomes a
WebSocket subprotocol.
