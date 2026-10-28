const DAY_MS = 24 * 60 * 60 * 1000;

export type DueInfo = {text: string; overdue: boolean; soon: boolean};

// Describes a due date relative to now. Done tasks are never overdue.
export function dueInfo(dueAt: number | null, done: boolean, now: number = Date.now()): DueInfo | null {
    if (dueAt === null) {
        return null;
    }
    const date = new Date(dueAt);
    const startOfToday = new Date(now);
    startOfToday.setHours(0, 0, 0, 0);
    const days = Math.floor((new Date(dueAt).setHours(0, 0, 0, 0) - startOfToday.getTime()) / DAY_MS);

    let text: string;
    if (days === 0) {
        text = 'Due today';
    } else if (days === 1) {
        text = 'Due tomorrow';
    } else if (days === -1) {
        text = 'Due yesterday';
    } else if (days < 0) {
        text = `${-days} days overdue`;
    } else {
        text = `Due ${date.toLocaleDateString(undefined, {month: 'short', day: 'numeric'})}`;
    }

    return {text, overdue: !done && dueAt < now, soon: !done && dueAt >= now && days <= 1};
}

// Due dates are picked as a calendar day and stored as the end of that local day,
// so "due today" is not overdue until the day is over.
export function dateInputToDueAt(value: string): number | null {
    const m = (/^(\d{4})-(\d{2})-(\d{2})$/).exec(value);
    if (!m) {
        return null;
    }
    return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 23, 59, 59, 0).getTime();
}

export function dueAtToDateInput(dueAt: number | null): string {
    if (dueAt === null) {
        return '';
    }
    const d = new Date(dueAt);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

const MAX_TITLE = 200;

// Builds a task title from a post: first non-empty line, markdown noise stripped, capped.
export function titleFromMessage(message: string): string {
    const line = message.split('\n').map((l) => l.trim()).find((l) => l !== '') || '';
    const cleaned = line.replace(/^(#{1,6}\s+|>\s*|[-*]\s+|\d+\.\s+)/, '').trim();
    if (cleaned.length <= MAX_TITLE) {
        return cleaned;
    }
    return cleaned.slice(0, MAX_TITLE - 1).trimEnd() + '…';
}

export function postPermalink(origin: string, postId: string): string {
    return `${origin}/_redirect/pl/${postId}`;
}
