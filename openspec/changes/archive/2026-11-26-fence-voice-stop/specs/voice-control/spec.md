## MODIFIED Requirements

### Requirement: Workflow commands return a job and a spoken confirmation

Starting, stopping, resuming, and sending input SHALL reuse the existing task create, stop, resume, and session-input paths. Each accepted call SHALL return a job handle and a one-sentence confirmation without waiting for a long-running start to finish. `wait` and `status` SHALL report that job. `wait` SHALL accept a timeout and SHALL NOT block longer than three seconds. When the job is unfinished at the deadline, the speech SHALL be `Still running.` Sending input SHALL require a task id. Stop SHALL NOT run unless confirm is true. Resuming a closed-out workflow SHALL NOT force revival unless confirm or force is set.

The session owner SHALL arbitrate a confirmed stop against pending starts and restarts before selecting the stop target. During a launch, the call SHALL either wait for a usable session or report that it cannot stop the session yet.

#### Scenario: Stop without confirm does nothing

- **WHEN** stop is requested without confirm
- **THEN** the session is not stopped and the speech asks for confirm

#### Scenario: A short wait does not block

- **WHEN** wait is called for a job that is still running and the timeout is longer than the cap
- **THEN** the call returns within the cap and the speech is `Still running.`

#### Scenario: Start of a new workflow needs a project and a prompt

- **WHEN** start names a workflow that does not exist and omits the project or the prompt
- **THEN** no task is created and the speech asks for both

#### Scenario: Send targets a task id

- **WHEN** send is requested with a name and no task id
- **THEN** nothing is written to a session and the speech says send needs a task id
