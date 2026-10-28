import {STATUSES} from 'taskTypes';
import type {Status, Task} from 'taskTypes';

// The Done column can grow without bound, so the board shows only the latest few.
export const DONE_VISIBLE = 25;

const GAP = 1000;

// Groups tasks into one list per status. Open columns keep board order (position, then age);
// Done lists the most recently completed first.
export function groupByStatus(tasks: Task[]): Record<Status, Task[]> {
    const groups = Object.fromEntries(STATUSES.map((s) => [s.key, [] as Task[]])) as Record<Status, Task[]>;
    for (const t of tasks) {
        groups[t.status].push(t);
    }
    for (const s of STATUSES) {
        if (s.key === 'done') {
            groups.done.sort((a, b) => (b.completed_at ?? b.updated_at) - (a.completed_at ?? a.updated_at));
        } else {
            groups[s.key].sort((a, b) => a.position - b.position || a.created_at - b.created_at);
        }
    }
    return groups;
}

// Picks a position that sorts between two neighbours. Positions are floats, so a
// card can be dropped anywhere without renumbering its neighbours.
export function positionBetween(prev: number | undefined, next: number | undefined): number {
    if (prev === undefined && next === undefined) {
        return GAP;
    }
    if (prev === undefined) {
        return (next as number) - GAP;
    }
    if (next === undefined) {
        return prev + GAP;
    }
    return (prev + next) / 2;
}

// The position for a card dropped at `index` among the column's other cards.
export function dropPosition(column: Task[], draggedId: string, index: number): number {
    const others = column.filter((t) => t.id !== draggedId);
    const i = Math.max(0, Math.min(index, others.length));
    return positionBetween(others[i - 1]?.position, others[i]?.position);
}

// Index at which a card dropped at `clientY` should land: the number of cards whose
// vertical midpoint is above the pointer. `midYs` must exclude the dragged card.
export function insertionIndex(midYs: number[], clientY: number): number {
    return midYs.filter((y) => y < clientY).length;
}
