import { createServer, type Server } from 'node:http';

// The fixture calendar feed the e2e build's calendar client fetches from 127.0.0.1. Only an e2e
// build (go build -tags e2e) may reach it, and only on a feed port (80, 443, 8080 or 8443).
export const feedPort = Number(process.env.E2E_FEED_PORT ?? '8080');

export const feedURL = (name: string) => `http://127.0.0.1:${String(feedPort)}/${name}.ics`;

const stamp = (d: Date) => d.toISOString().replace(/[-:]/g, '').replace(/\.\d{3}/, '');

// A weekly lesson from tomorrow at 15:00 UTC, three times, titled after the feed's name.
function feed(name: string): string {
  const start = new Date();
  start.setUTCDate(start.getUTCDate() + 1);
  start.setUTCHours(15, 0, 0, 0);
  const end = new Date(start.getTime() + 60 * 60 * 1000);
  return [
    'BEGIN:VCALENDAR', 'VERSION:2.0', 'PRODID:-//Example//Fixture//EN',
    'BEGIN:VEVENT', `UID:${name}@example.test`, `DTSTAMP:${stamp(new Date())}`,
    `DTSTART:${stamp(start)}`, `DTEND:${stamp(end)}`, 'RRULE:FREQ=WEEKLY;COUNT=3',
    `SUMMARY:Swimming ${name}`, 'END:VEVENT', 'END:VCALENDAR', '',
  ].join('\r\n');
}

export function serveFeeds(): Promise<Server> {
  const server = createServer((req, res) => {
    const m = /^\/([\w-]+)\.ics$/.exec(req.url ?? '');
    if (!m?.[1]) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/calendar' }).end(feed(m[1]));
  });
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(feedPort, '127.0.0.1', () => { resolve(server); });
  });
}
