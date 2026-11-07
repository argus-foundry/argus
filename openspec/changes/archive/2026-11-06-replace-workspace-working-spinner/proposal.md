# Replace the workspace working spinner

## Why
The rotating rail spinner is distracting. The Scribe conveys ongoing work through a small pen nib writing three short lines.

## What Changes
Replace only the working SVG glyph with the Scribe. The nib writes three lines, lifts, then returns to the start over a 5.6 second loop. Preserve the fixed 16px slot, currentColor, 1.4px rounded strokes, accessible Running label, tooltip and status precedence. Animate only transform, opacity and stroke-dashoffset. Reduced motion keeps the completed lines and stationary nib.

## Impact
This is local to the optional desktop web workspace. No REST contract or Go changes; TUI, existing web SPA and macOS retain their current indicators. Existing browser assertions remain valid through a visually constant opacity animation on the working SVG itself, disabled with all child animation under reduced motion.
