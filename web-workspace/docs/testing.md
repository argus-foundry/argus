# Workspace test commands

All commands below start at the repository root. Browser verification follows `web-tests/playwright.config.ts`, using the same package dependency, serial Playwright runner, failure artifacts and owned `webServer`. Browser suites remain manual and are not wired into CI or the Makefile.

## Unit tests

```sh
npm test --prefix web-workspace
```

The `*.test.mjs` suites stay under `web-workspace/tests/` and use Node's test runner. They require Node 20 or newer and no dependency install. CI and `make pre-pr` include these tests; `make workspace-test` runs them on their own.

## Browser tests

```sh
go build -o /tmp/argus-test-server ./cmd/argus-test-server
npm ci --prefix web-tests
(cd web-tests && npx playwright install chromium webkit)
(cd web-tests && npx playwright test -c playwright.workspace.config.ts)
(cd web-tests && npx playwright test -c playwright.workspace-security.config.ts)
```

The workspace config starts a fresh isolated harness on 17744 and the workspace proxy on 17750. The harness reset endpoint uses 17754. Existing servers are never reused. The config fixes test origins and token fixtures; it does not accept an external daemon origin. Live-write suites also check for the harness's temporary worktree before writing.

The security config runs separately after those servers exit. Its spec owns a synthetic upstream and workspace proxy on 17744 and 17750, tests actual proxy response headers, and closes only those servers. It never contacts a daemon. Its `build.external` setting leaves the existing ESM development server unchanged when imported from a TypeScript spec.

To run one suite, pass its path relative to `web-tests/`.

```sh
(cd web-tests && npx playwright test -c playwright.workspace.config.ts tests/workspace/composer/attachment.spec.ts)
```

Specs use desktop Chromium contexts and explicit responsive viewport checks. They use Playwright browser fixtures and hooks. Failure artifacts use `web-tests/test-results/`. Explicit workflow screenshots remain under `.context/`.

## Existing master tests

```sh
(cd web-tests && ARGUS_TEST_PORT=17744 npx playwright test)
```

The master iphone and desktop projects run separately against a fresh instance of the same harness binary. The master config excludes workspace specs so the two frontends retain independent test selection.
