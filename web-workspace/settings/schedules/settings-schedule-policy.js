export function localDateTime(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  return new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,23);
}
export function schedulePayload(values, original = {}, now = Date.now()) {
  const body = {name:values.name.trim(),project:values.project.trim(),backend:(values.backend || '').trim(),model:(values.model || '').trim(),prompt:values.prompt,enabled:!!values.enabled};
  if (!body.name || !body.project || !body.prompt.trim()) throw new Error('Name, project and prompt are required.');
  if (values.cadence === 'once') {
    const time = new Date(values.runOnceAt), previous = new Date(original.run_once_at);
    if (!Number.isFinite(time.getTime())) throw new Error('Choose a valid one-shot time.');
    if (time.getTime() !== previous.getTime()) {
      if (time.getTime() <= now) throw new Error('The one-shot time must be in the future.');
      body.run_once_at = time.toISOString();
    }
  } else {
    const schedule = values.schedule.trim();
    if (!schedule) throw new Error('Cron schedule is required.');
    if (schedule !== original.schedule) body.schedule = schedule;
  }
  if (new TextEncoder().encode(JSON.stringify(body)).length > 1024*1024) throw new Error('Schedule exceeds the existing 1 MiB limit. Shorten the prompt.');
  return body;
}
