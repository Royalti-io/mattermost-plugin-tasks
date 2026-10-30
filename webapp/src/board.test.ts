import {DONE_VISIBLE, dropPosition, groupByStatus, insertionIndex, positionBetween} from 'board';
import type {Task} from 'taskTypes';

const t = (id: string, over: Partial<Task> = {}): Task => ({
    id,
    channel_id: 'c1',
    title: id,
    description: '',
    status: 'todo',
    priority: 'normal',
    due_at: null,
    position: 1000,
    created_by: 'u1',
    created_at: 1,
    updated_at: 1,
    completed_at: null,
    assignees: [],
    labels: [],
    checklist: [],
    private: false,
    ...over,
});

describe('groupByStatus', () => {
    it('creates every column, even empty ones', () => {
        const g = groupByStatus([]);
        expect(Object.keys(g)).toEqual(['backlog', 'todo', 'in_progress', 'blocked', 'done']);
        expect(g.blocked).toEqual([]);
    });

    it('orders open columns by position, then age', () => {
        const g = groupByStatus([
            t('c', {position: 3000}),
            t('b', {position: 1000, created_at: 2}),
            t('a', {position: 1000, created_at: 1}),
        ]);
        expect(g.todo.map((x) => x.id)).toEqual(['a', 'b', 'c']);
    });

    it('lists done tasks most recently completed first', () => {
        const g = groupByStatus([
            t('old', {status: 'done', completed_at: 10}),
            t('new', {status: 'done', completed_at: 99}),
            t('mid', {status: 'done', completed_at: 50}),
        ]);
        expect(g.done.map((x) => x.id)).toEqual(['new', 'mid', 'old']);
    });

    it('exposes a sane Done cap', () => {
        expect(DONE_VISIBLE).toBeGreaterThan(0);
    });
});

describe('positionBetween', () => {
    it('handles an empty column, the top, the bottom and the middle', () => {
        expect(positionBetween(undefined, undefined)).toBe(1000);
        expect(positionBetween(undefined, 1000)).toBe(0);
        expect(positionBetween(2000, undefined)).toBe(3000);
        expect(positionBetween(1000, 2000)).toBe(1500);
    });

    it('can go below zero when dropped above the first card', () => {
        expect(positionBetween(undefined, 0)).toBe(-1000);
    });
});

describe('dropPosition', () => {
    const col = [t('a', {position: 1000}), t('b', {position: 2000}), t('c', {position: 3000})];

    it('drops between neighbours', () => {
        expect(dropPosition(col, 'x', 1)).toBe(1500);
    });

    it('drops at the top and the end', () => {
        expect(dropPosition(col, 'x', 0)).toBe(0);
        expect(dropPosition(col, 'x', 3)).toBe(4000);
    });

    it('ignores the dragged card when reordering inside a column', () => {
        // Moving "a" to the slot after "b": others are [b, c], index 1 sits between them.
        expect(dropPosition(col, 'a', 1)).toBe(2500);
    });

    it('clamps an out-of-range index', () => {
        expect(dropPosition(col, 'x', 99)).toBe(4000);
        expect(dropPosition(col, 'x', -5)).toBe(0);
    });

    it('works for an empty column', () => {
        expect(dropPosition([], 'x', 0)).toBe(1000);
    });
});

describe('insertionIndex', () => {
    it('counts cards whose midpoint is above the pointer', () => {
        const mids = [50, 150, 250];
        expect(insertionIndex(mids, 10)).toBe(0);
        expect(insertionIndex(mids, 100)).toBe(1);
        expect(insertionIndex(mids, 200)).toBe(2);
        expect(insertionIndex(mids, 900)).toBe(3);
        expect(insertionIndex([], 5)).toBe(0);
    });
});
