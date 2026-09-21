# Native-browser identity exchange

The normal full suite generates two revisions of one declaration, compiles
`browser.ts` against their TypeScript adapters, and compiles the generated Go
endpoint with `browser_host_test.go`. The revisions differ only by an optional
record field, so the fixture's payload fits both while their identities differ.
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
compiled modules and sixteen real WebSocket endpoints on a loopback-only test
server, waits for the page's report, and stops the browser. No browser sandbox
flags are changed. The report includes the actual browser user agent.

Each of matching digest, differing digest, absent `identity.check`, and a peer
with no digest is exercised through generated `prepareFromWire`/`complete` and
generated `fromWire`, with no subprotocol and with the consumer's `ticket.fixture`
subprotocol. The page uses `new WebSocket` from its browser global. Matching and
mismatching peers are generated Go `ToWire` endpoints forwarded onto the socket;
the absent and digestless peers are explicit untyped controls.

Preparation is registered before connecting. The Go peer immediately sends a
reverse request and an event. A native WebSocket listener observes both frames
before `complete` begins, while the generated receiver has no model. No receiver
may execute before the returned factory is bound. After a match, each early
operation executes once; after a mismatch, neither executes and the reverse
request is refused. The `fromWire` rows exercise its ordinary asynchronous
composition without early traffic.

The Go server independently counts application invocations and reverse
completions and records offered subprotocols. Every mismatch must preserve the
exact `contract_mismatch` diagnostic and make zero application calls; each
accepted row makes one generated application call. The report includes native
frame and callback counts. Identity remains an ordinary RPC, with the consumer's
WebSocket subprotocol choices unchanged.
