export type Status = 'backlog' | 'todo' | 'in_progress' | 'blocked' | 'done';
export type Priority = 'low' | 'normal' | 'high' | 'critical';

export type ChecklistItem = {
    id: string;
    text: string;
    done: boolean;
    position: number;
};

export type Task = {
    id: string;
    channel_id: string;
    title: string;
    description: string;
    status: Status;
    priority: Priority;
    due_at: number | null;
    position: number;
    created_by: string;
    created_at: number;
    updated_at: number;
    completed_at: number | null;
    post_id?: string;
    source?: string;
    external_id?: string;
    assignees: string[];
    labels: string[];
    checklist: ChecklistItem[];

    // Private tasks are visible only to their creator and assignees.
    private: boolean;
};

export const STATUSES: Array<{key: Status; label: string}> = [
    {key: 'backlog', label: 'Backlog'},
    {key: 'todo', label: 'To do'},
    {key: 'in_progress', label: 'In progress'},
    {key: 'blocked', label: 'Blocked'},
    {key: 'done', label: 'Done'},
];

export const PRIORITIES: Array<{key: Priority; label: string}> = [
    {key: 'low', label: 'Low'},
    {key: 'normal', label: 'Normal'},
    {key: 'high', label: 'High'},
    {key: 'critical', label: 'Critical'},
];

export const OPEN_STATUSES: Status[] = ['backlog', 'todo', 'in_progress', 'blocked'];
