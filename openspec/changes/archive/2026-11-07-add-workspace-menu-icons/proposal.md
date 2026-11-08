# Workspace menu icons

## Why
Action menus need a consistent visual cue for each action, including destructive actions.

## What changes
Use one inline SVG stroke icon set for every task, coordinator and Changes file menu item and Settings navigation entry except repository identity avatars. Align icons in a fixed leading column. Separate destructive entries with a divider and inherit their red text color. Preserve labels, accessible names and keyboard interactions. No dependency or backend changes.

## Abstraction inventory
| Name | Type | Contains | Depends on |
| --- | --- | --- | --- |
| Menu icon | Visual atom | Named monochrome paths | Current text color |
| Menu item | Visual component | Icon and existing label | Menu icon |
| Destructive item | Menu item variant | Red icon and label, preceding divider | Menu item, error and border tokens |
| Settings navigation entry | Visual component | Icon and existing section label | Menu icon |

## Non-goals and parity
Native form select/option controls retain browser rendering and keyboard behavior. Project headers have direct Configuration/New task buttons; header and composer have direct controls rather than action menus. The split task picker is a native form select and its roster is task navigation. No new menus are introduced. TUI and macOS keep their existing native presentation; this is an optional workspace visual change with no REST contract changes.
