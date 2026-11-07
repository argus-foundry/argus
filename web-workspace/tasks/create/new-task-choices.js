export function creationBackend(backends, selected) {
  return backends.find(backend => backend.name === selected) || null;
}

export function creationOverrides(model, sandbox, backend) {
  const value = model.trim();
  if (value && !backend) throw new Error('Choose a backend before choosing a model.');
  if (value && backend.models?.length && !backend.models.includes(value)) throw new Error('Choose a listed model for this backend.');
  return {
    ...(value ? {model: value} : {}),
    ...(['enabled', 'disabled'].includes(sandbox) ? {sandbox_override: sandbox} : {}),
  };
}
