import {dateInputToDueAt, dueAtToDateInput, dueInfo, postPermalink, titleFromMessage} from 'format';

const noon = (y: number, m: number, d: number) => new Date(y, m - 1, d, 12, 0, 0).getTime();

describe('dueInfo', () => {
    const now = noon(2026, 10, 28);

    it('returns null without a due date', () => {
        expect(dueInfo(null, false, now)).toBeNull();
    });

    it('labels today, tomorrow and yesterday', () => {
        expect(dueInfo(dateInputToDueAt('2026-10-28'), false, now)?.text).toBe('Due today');
        expect(dueInfo(dateInputToDueAt('2026-10-29'), false, now)?.text).toBe('Due tomorrow');
        expect(dueInfo(dateInputToDueAt('2026-10-27'), false, now)?.text).toBe('Due yesterday');
    });

    it('does not mark a task due later today as overdue, since the due date is end of day', () => {
        const info = dueInfo(dateInputToDueAt('2026-10-28'), false, now);
        expect(info?.overdue).toBe(false);
        expect(info?.soon).toBe(true);
    });

    it('marks past due dates overdue unless the task is done', () => {
        const past = dateInputToDueAt('2026-10-20');
        expect(dueInfo(past, false, now)).toMatchObject({overdue: true, text: '8 days overdue'});
        expect(dueInfo(past, true, now)?.overdue).toBe(false);
    });

    it('is not soon for a far-off date', () => {
        expect(dueInfo(dateInputToDueAt('2026-12-25'), false, now)?.soon).toBe(false);
    });
});

describe('due date input conversion', () => {
    it('round-trips a calendar day', () => {
        const ms = dateInputToDueAt('2026-03-09');
        expect(ms).not.toBeNull();
        expect(dueAtToDateInput(ms)).toBe('2026-03-09');
    });

    it('stores the end of the local day', () => {
        const d = new Date(dateInputToDueAt('2026-03-09') as number);
        expect([d.getHours(), d.getMinutes(), d.getSeconds()]).toEqual([23, 59, 59]);
    });

    it('rejects blank or malformed input', () => {
        expect(dateInputToDueAt('')).toBeNull();
        expect(dateInputToDueAt('09/03/2026')).toBeNull();
        expect(dueAtToDateInput(null)).toBe('');
    });
});

describe('titleFromMessage', () => {
    it('uses the first non-empty line', () => {
        expect(titleFromMessage('\n\n  Fix the login bug  \nmore detail')).toBe('Fix the login bug');
    });

    it('strips leading markdown markers', () => {
        expect(titleFromMessage('## Ship v2')).toBe('Ship v2');
        expect(titleFromMessage('> quoted request')).toBe('quoted request');
        expect(titleFromMessage('- [ ] not a checkbox parser')).toBe('[ ] not a checkbox parser');
        expect(titleFromMessage('1. first step')).toBe('first step');
    });

    it('caps long titles at 200 characters', () => {
        const t = titleFromMessage('a'.repeat(500));
        expect(t.length).toBe(200);
        expect(t.endsWith('…')).toBe(true);
    });

    it('handles an empty message', () => {
        expect(titleFromMessage('')).toBe('');
    });
});

describe('postPermalink', () => {
    it('uses the redirect route', () => {
        expect(postPermalink('https://mm.example.com', 'abc')).toBe('https://mm.example.com/_redirect/pl/abc');
    });
});
