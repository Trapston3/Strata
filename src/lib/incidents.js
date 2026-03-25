function scoreIncident(events, ecosystemNoise) {
  const frequencyWeight = Math.min(events.length * 18, 45);
  const serviceSpread = new Set(events.map((event) => event.service)).size;
  const recentEventTime = Math.max(...events.map((event) => Date.parse(event.timestamp)));
  const minutesSinceLastEvent = Math.max(
    1,
    Math.round((Date.now() - recentEventTime) / 60_000)
  );
  const recencyWeight = Math.max(10, 35 - minutesSinceLastEvent * 2);

  return Math.min(100, frequencyWeight + recencyWeight + serviceSpread * 10 + ecosystemNoise * 3);
}

function classifySeverity(score) {
  if (score >= 80) return "critical";
  if (score >= 60) return "high";
  if (score >= 40) return "medium";
  return "low";
}

function buildSummary(group) {
  const latest = group.events[0];
  const repeated = group.events.length > 1 ? `repeated ${group.events.length} times` : "seen once";

  return `${latest.service} is showing ${latest.errorCode} failures, ${repeated}, with the most recent event at ${new Date(latest.timestamp).toLocaleTimeString("en-IN", { hour: "2-digit", minute: "2-digit" })}.`;
}

export function buildIncidents(logs) {
  const groups = new Map();

  for (const log of logs) {
    if (log.level !== "error") continue;

    const key = `${log.service}:${log.errorCode}`;
    const group = groups.get(key) ?? {
      id: key,
      service: log.service,
      errorCode: log.errorCode,
      events: []
    };

    group.events.push(log);
    groups.set(key, group);
  }

  const ecosystemNoise = new Set(logs.map((log) => log.service)).size;

  return [...groups.values()]
    .map((group) => {
      group.events.sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp));
      const score = scoreIncident(group.events, ecosystemNoise);

      return {
        id: group.id,
        service: group.service,
        errorCode: group.errorCode,
        eventCount: group.events.length,
        latestTimestamp: group.events[0].timestamp,
        score,
        severity: classifySeverity(score),
        summary: buildSummary(group)
      };
    })
    .sort((a, b) => b.score - a.score);
}
