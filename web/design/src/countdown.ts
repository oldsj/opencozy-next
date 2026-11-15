/** "24:59", "1:02:03", "0:00". */
export function countdownLabel(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor(total / 60) % 60;
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${String(h)}:${pad(m)}:${pad(s)}` : `${String(m)}:${pad(s)}`;
}

const unit = (n: number, one: string, many: string) => `${String(n)} ${n === 1 ? one : many}`;

/** What a screen reader hears on reaching a timer: whole minutes, rounded up. */
export function countdownSpoken(seconds: number): string {
  if (seconds <= 0) return 'Time’s up';
  const minutes = Math.ceil(seconds / 60);
  if (minutes >= 60) {
    const h = Math.floor(minutes / 60);
    const m = minutes % 60;
    return `${unit(h, 'hour', 'hours')}${m ? ` ${unit(m, 'minute', 'minutes')}` : ''} left`;
  }
  return `${unit(minutes, 'minute', 'minutes')} left`;
}
