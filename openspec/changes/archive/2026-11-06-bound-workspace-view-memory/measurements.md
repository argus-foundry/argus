# View memory measurements

## Method

Real xterm in Chromium, 100 tasks, 10,000 lines of 80-column output per task, full task snapshot retained and garbage collection before each measurement. Heap includes the connected app and excludes native/canvas allocations. API reads were mocked. These are recorded measurements, not a runtime memory guarantee.

## Terminal heap

| Measurement | Before MiB | After MiB |
| --- | ---: | ---: |
| Baseline | 4.03 | 4.16 |
| One cached task | 6.29 | 6.41 |
| 25 visited tasks | 55.72 | 21.01 |
| 100 visited tasks | 208.42 | 21.15 |
| Cache destroyed | 4.60 | 4.63 |

The retained controller count fell from 100 to eight. Eight allows two visible panes and six recent inactive terminals while bounding full-history xterm allocations. Total measured heap at 100 visits fell about 90 percent.

## Review memory limits

Inactive review selections retain path and layout mode without diff bodies. Revisiting fetches fresh text. No review heap measurement is claimed.
