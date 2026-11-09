## ADDED Requirements

### Requirement: Live panes honor synchronized output

While a live pane's output stream is inside an open DEC private mode 2026 block (`CSI ?2026h` without its `CSI ?2026l`), the pane SHALL NOT feed that block to its emulator and SHALL keep painting the last completed frame. The block SHALL be fed and painted as soon as it closes. Sequences split across read boundaries SHALL be recognized. A block still open after 150ms, or holding more than 128KiB, SHALL be flushed so a missing `?2026l` can never freeze a pane; the flush SHALL be logged at most once per 10 seconds per pane. The hold SHALL be dropped on every reset or rebind of the live emulator. Replay and preview paths are unaffected.

#### Scenario: Frame split across reads

- **WHEN** a `?2026h` frame has arrived without its `?2026l`
- **THEN** the pane SHALL keep painting the previous frame, and SHALL paint the new frame in one step once the close arrives

#### Scenario: Sequence split across chunks

- **WHEN** the opening sequence itself is split between two reads
- **THEN** the partial prefix SHALL be withheld until it resolves

#### Scenario: Lost close

- **WHEN** no `?2026l` arrives within 150ms of the block opening
- **THEN** the withheld bytes SHALL be fed and painted, driven by a timer wake with no output required

#### Scenario: Rebind during a block

- **WHEN** the pane is reset or rebound while holding an open block
- **THEN** no hold SHALL survive into the new session

#### Scenario: Echo latency

- **WHEN** a key was pressed recently and its echo frame closes
- **THEN** the frame SHALL paint under the fast echo cap, adding no latency beyond the frame the agent was already drawing

#### Scenario: Writes inside a block do not cost a frame

- **WHEN** a write begins and ends inside the same open block
- **THEN** the repaint pump SHALL NOT be woken for it
