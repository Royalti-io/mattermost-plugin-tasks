import manifest from 'manifest';
import type {ChecklistItem, Priority, Status, Task} from 'taskTypes';

const base = `/plugins/${manifest.id}/api/v1`;
const coreBase = '/api/v4';

export class ApiError extends Error {
    status: number;

    constructor(status: number, message: string) {
        super(message);
        this.status = status;
    }
}

function csrfToken(): string {
    const match = document.cookie.match(/(?:^|;\s*)MMCSRF=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : '';
}

export function buildQuery(params: Record<string, string | number | undefined>): string {
    const q = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
        if (value !== undefined && value !== '') {
            q.set(key, String(value));
        }
    }
    const s = q.toString();
    return s ? `?${s}` : '';
}

async function request<T>(method: string, path: string, body?: unknown, prefix: string = base): Promise<T> {
    const headers: Record<string, string> = {'X-Requested-With': 'XMLHttpRequest'};
    if (method !== 'GET') {
        headers['X-CSRF-Token'] = csrfToken();
    }
    if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
    }

    const resp = await fetch(prefix + path, {
        method,
        headers,
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
    });

    if (resp.status === 204) {
        return undefined as T;
    }
    const text = await resp.text();
    let parsed: unknown;
    try {
        parsed = text ? JSON.parse(text) : undefined;
    } catch {
        parsed = undefined;
    }
    if (!resp.ok) {
        const message = (parsed as {error?: string} | undefined)?.error || `Request failed (${resp.status})`;
        throw new ApiError(resp.status, message);
    }
    return parsed as T;
}

export type ListParams = {
    channel_id?: string;
    assignee?: string;
    status?: string;
    q?: string;
    limit?: number;
};

export type CreateParams = {
    channel_id: string;
    title: string;
    description?: string;
    status?: Status;
    priority?: Priority;
    due_at?: number | null;
    assignees?: string[];
    labels?: string[];
    checklist?: string[];
    post_id?: string;
    notify_thread?: boolean;

    // Omit to use the user's "private by default" setting.
    private?: boolean;
};

export type PatchParams = Partial<{
    title: string;
    description: string;
    status: Status;
    priority: Priority;
    due_at: number | null;
    assignees: string[];
    labels: string[];
    private: boolean;
}>;

export type Prefs = {private_by_default: boolean};

export const api = {
    async listTasks(params: ListParams): Promise<Task[]> {
        const res = await request<{tasks: Task[]}>('GET', '/tasks' + buildQuery(params));
        return res.tasks;
    },
    createTask: (input: CreateParams) => request<Task>('POST', '/tasks', input),
    updateTask: (id: string, patch: PatchParams) => request<Task>('PATCH', `/tasks/${id}`, patch),
    moveTask: (id: string, status: Status, position?: number) => request<Task>('POST', `/tasks/${id}/move`, {status, position}),
    deleteTask: (id: string) => request<void>('DELETE', `/tasks/${id}`),
    addChecklistItem: (taskId: string, text: string) => request<ChecklistItem>('POST', `/tasks/${taskId}/checklist`, {text}),
    updateChecklistItem: (taskId: string, itemId: string, patch: {text?: string; done?: boolean}) =>
        request<ChecklistItem>('PATCH', `/tasks/${taskId}/checklist/${itemId}`, patch),
    deleteChecklistItem: (taskId: string, itemId: string) => request<void>('DELETE', `/tasks/${taskId}/checklist/${itemId}`),
    getPrefs: () => request<Prefs>('GET', '/prefs'),
    updatePrefs: (patch: Partial<Prefs>) => request<Prefs>('PATCH', '/prefs', patch),
};

// The slice of a Mattermost user the picker needs.
export type MMChannel = {id: string; type: string; name: string; display_name: string; delete_at: number};

export type MMUser = {
    id: string;
    username: string;
    first_name?: string;
    last_name?: string;
    nickname?: string;
    is_bot?: boolean;
};

// Mattermost's own user API, used to find and name assignees.
export const userApi = {

    // Members of a channel whose name starts with `name`. Assignees must be channel members.
    async autocomplete(name: string, channelId: string, teamId?: string): Promise<MMUser[]> {
        const q = buildQuery({in_channel: channelId, in_team: teamId, name, limit: 10});
        const res = await request<{users?: MMUser[]}>('GET', '/users/autocomplete' + q, undefined, coreBase);
        return res.users || [];
    },
    byIds: (ids: string[]) => request<MMUser[]>('POST', '/users/ids', ids, coreBase),

    // Every channel the user belongs to, across teams. Used when the page was opened
    // directly (no team in the URL), so Mattermost has not loaded any channels yet.
    myChannels: () => request<MMChannel[]>('GET', '/users/me/channels', undefined, coreBase),
};
