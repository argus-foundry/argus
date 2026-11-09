## Implementation

- [x] Expose cache-only Claude and Codex usage readings through an authenticated API response.
- [x] Add a Usage section to web Settings showing Claude account five-hour/weekly and Codex weekly percentages and reset times, including unknown states.
- [x] Refresh readings with the existing Settings lifecycle.
- [x] Bump the service worker shell version after editing the web app asset.
- [x] Keep the latest valid Claude window per duration when bridge and probe caches contain partial readings.
- [x] Add API and browser coverage for populated, unavailable, reset-less, expired, and out-of-order readings.
- [x] Document the usage display/cache behavior in the gotcha file, index, and REST API reference.
