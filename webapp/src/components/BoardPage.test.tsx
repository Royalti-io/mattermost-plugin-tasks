/**
 * @jest-environment jsdom
 */
import {createEvent, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {api, userApi} from 'api';
import React from 'react';
import type {Task} from 'taskTypes';

import BoardPage from './BoardPage';

jest.mock('api', () => ({
    api: {
        listTasks: jest.fn(),
        createTask: jest.fn(),
        updateTask: jest.fn(),
        moveTask: jest.fn(),
        deleteTask: jest.fn(),
        addChecklistItem: jest.fn(),
        updateChecklistItem: jest.fn(),
        deleteChecklistItem: jest.fn(),
    },
    userApi: {autocomplete: jest.fn().mockResolvedValue([]), byIds: jest.fn().mockResolvedValue([]), myChannels: jest.fn().mockResolvedValue([])},
}));

const state = {
    entities: {
        users: {currentUserId: 'u1', profiles: {u1: {username: 'ada'}}},
        channels: {
            currentChannelId: 'c1',
            channels: {
                c1: {id: 'c1', type: 'O', display_name: 'General', name: 'general', delete_at: 0},
                c2: {id: 'c2', type: 'P', display_name: 'Ops', name: 'ops', delete_at: 0},
                c3: {id: 'c3', type: 'O', display_name: 'Not mine', name: 'nm', delete_at: 0},
                c4: {id: 'c4', type: 'D', display_name: '', name: 'u1__u2', delete_at: 0},
            },
            myMembers: {c1: {}, c2: {}, c4: {}},
        },
    },
};

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn(state),
}));

const mocked = api as jest.Mocked<typeof api>;

const task = (id: string, over: Partial<Task> = {}): Task => ({
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
    ...over,
});

const column = (name: string) => screen.getByRole('region', {name});

// jsdom has no layout, so drag events carry a minimal dataTransfer and a clientY.
const dt = () => ({data: {} as Record<string, string>,
    setData(k: string, v: string) {
        this.data[k] = v;
    },
    getData(k: string) {
        return this.data[k];
    },
    effectAllowed: '',
    dropEffect: ''});

// jsdom's drag events ignore clientY in their init, so it is defined on the event afterwards.
function dragEvent(type: 'dragOver' | 'drop', target: HTMLElement, dataTransfer: unknown, clientY: number) {
    const ev = createEvent[type](target, {dataTransfer});
    Object.defineProperty(ev, 'clientY', {value: clientY});
    fireEvent(target, ev);
}

function drag(card: HTMLElement, target: HTMLElement, clientY: number) {
    const dataTransfer = dt();
    fireEvent.dragStart(card, {dataTransfer});
    dragEvent('dragOver', target, dataTransfer, clientY);
    dragEvent('drop', target, dataTransfer, clientY);
}

beforeEach(() => {
    jest.resetAllMocks();
    window.localStorage.clear();
    mocked.listTasks.mockResolvedValue([
        task('write', {status: 'todo', position: 1000}),
        task('review', {status: 'todo', position: 2000}),
        task('build', {status: 'in_progress', position: 1000, priority: 'high'}),
        task('shipped', {status: 'done', completed_at: 5}),
    ]);
});

describe('BoardPage', () => {
    it('shows every column with its tasks, scoped to the open channel', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');

        expect(mocked.listTasks).toHaveBeenCalledWith(expect.objectContaining({channel_id: 'c1', limit: 500}));
        expect(within(column('To do')).getByText('write')).toBeTruthy();
        expect(within(column('To do')).getByText('review')).toBeTruthy();
        expect(within(column('In progress')).getByText('build')).toBeTruthy();
        expect(within(column('Done')).getByText('shipped')).toBeTruthy();
        expect(within(column('Blocked')).getByText('Nothing here')).toBeTruthy();
        expect(within(column('To do')).getByText('2')).toBeTruthy();
    });

    it('offers only public and private channels the user belongs to', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');
        const options = within(screen.getByLabelText('Channel')).getAllByRole('option').map((o) => o.textContent);
        expect(options).toEqual(['~General', '~Ops']);
    });

    it('loads the user\'s channels when the page is opened directly and the store has none', async () => {
        const saved = state.entities.channels;
        state.entities.channels = {currentChannelId: '', channels: {}, myMembers: {}} as unknown as typeof saved;
        (userApi.myChannels as jest.Mock).mockResolvedValue([
            {id: 'c2', type: 'P', display_name: 'Ops', name: 'ops', delete_at: 0},
            {id: 'c1', type: 'O', display_name: 'General', name: 'general', delete_at: 0},
            {id: 'c4', type: 'D', display_name: '', name: 'u1__u2', delete_at: 0},
            {id: 'c9', type: 'O', display_name: 'Old', name: 'old', delete_at: 5},
        ]);
        try {
            render(<BoardPage/>);
            await screen.findByText('write');
            const options = within(screen.getByLabelText('Channel')).getAllByRole('option').map((o) => o.textContent);
            expect(options).toEqual(['~General', '~Ops']);
            expect(mocked.listTasks).toHaveBeenLastCalledWith(expect.objectContaining({channel_id: 'c1'}));
            expect(screen.queryByText('Join a channel to see its tasks.')).toBeNull();
        } finally {
            state.entities.channels = saved;
        }
    });

    it('remembers the chosen channel and reloads for it', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');

        fireEvent.change(screen.getByLabelText('Channel'), {target: {value: 'c2'}});

        await waitFor(() => expect(mocked.listTasks).toHaveBeenLastCalledWith(expect.objectContaining({channel_id: 'c2'})));
        expect(window.localStorage.getItem('rt-board-channel')).toBe('c2');
    });

    it('switches to the cross-channel scope and shows channel names', async () => {
        mocked.listTasks.mockResolvedValue([task('elsewhere', {channel_id: 'c2'})]);
        render(<BoardPage/>);
        await screen.findByText('elsewhere');
        expect(within(column('To do')).queryByText('~Ops')).toBeNull();

        fireEvent.change(screen.getByLabelText('Scope'), {target: {value: 'all'}});

        expect(await within(column('To do')).findByText('~Ops')).toBeTruthy();
        const last = mocked.listTasks.mock.calls[mocked.listTasks.mock.calls.length - 1][0];
        expect(last).not.toHaveProperty('channel_id');
        expect(screen.queryByLabelText('Channel')).toBeNull();
        expect(screen.queryByText('+ Add task')).toBeNull();
    });

    it('moves a card to another column by dropping it, appending at the end', async () => {
        mocked.moveTask.mockResolvedValue(task('write', {status: 'in_progress', position: 2000}));
        render(<BoardPage/>);
        await screen.findByText('write');

        drag(screen.getByRole('button', {name: 'write'}), column('In progress'), 9999);

        await waitFor(() => expect(mocked.moveTask).toHaveBeenCalledWith('write', 'in_progress', 2000));
    });

    it('drops above the first card using a position before it', async () => {
        mocked.moveTask.mockResolvedValue(task('review', {position: 0}));
        render(<BoardPage/>);
        await screen.findByText('write');

        drag(screen.getByRole('button', {name: 'review'}), column('To do'), -1);

        await waitFor(() => expect(mocked.moveTask).toHaveBeenCalledWith('review', 'todo', 0));
    });

    it('lets the server place a card dropped on Done', async () => {
        mocked.moveTask.mockResolvedValue(task('write', {status: 'done', completed_at: 9}));
        render(<BoardPage/>);
        await screen.findByText('write');

        drag(screen.getByRole('button', {name: 'write'}), column('Done'), 9999);

        await waitFor(() => expect(mocked.moveTask).toHaveBeenCalledWith('write', 'done', undefined));
    });

    it('does nothing when a card is dropped where it already is', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');

        // "write" is first; dropping it above the first card keeps its place only if the position is unchanged.
        drag(screen.getByRole('button', {name: 'build'}), column('In progress'), -1);

        await new Promise((r) => setTimeout(r, 20));
        expect(mocked.moveTask).not.toHaveBeenCalled();
    });

    it('reverts and shows an error when a move fails', async () => {
        mocked.moveTask.mockRejectedValue(new Error('you cannot change tasks in this channel'));
        render(<BoardPage/>);
        await screen.findByText('write');

        drag(screen.getByRole('button', {name: 'write'}), column('Blocked'), 9999);

        expect((await screen.findByRole('alert')).textContent).toBe('you cannot change tasks in this channel');
        await waitFor(() => expect(mocked.listTasks.mock.calls.length).toBeGreaterThan(1));
    });

    it('adds a task to a specific column', async () => {
        mocked.createTask.mockResolvedValue(task('new'));
        render(<BoardPage/>);
        await screen.findByText('write');

        fireEvent.click(within(column('Blocked')).getByText('+ Add task'));
        const input = screen.getByLabelText('New task in Blocked');
        fireEvent.change(input, {target: {value: ' Waiting on legal '}});
        fireEvent.keyDown(input, {key: 'Enter'});

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalledWith({
            channel_id: 'c1',
            title: 'Waiting on legal',
            status: 'blocked',
        }));
    });

    it('does not offer quick add on the Done column', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');
        expect(within(column('Done')).queryByText('+ Add task')).toBeNull();
    });

    it('opens a card in a dialog with the editable detail, and closes it on Escape', async () => {
        render(<BoardPage/>);
        await screen.findByText('write');

        fireEvent.click(screen.getByRole('button', {name: 'write'}));
        const dialog = await screen.findByRole('dialog');
        expect(within(dialog).getByLabelText('Title')).toBeTruthy();
        expect(within(dialog).getByLabelText('Description')).toBeTruthy();

        fireEvent.keyDown(document, {key: 'Escape'});
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('caps the Done column and reveals the rest on request', async () => {
        const many = Array.from({length: 30}, (_, i) => task(`d${i}`, {status: 'done', completed_at: i}));
        mocked.listTasks.mockResolvedValue(many);
        render(<BoardPage/>);
        await screen.findByText('d29');

        expect(within(column('Done')).getAllByRole('button', {name: /^d\d+$/})).toHaveLength(25);
        fireEvent.click(screen.getByText('Show 5 more'));
        expect(within(column('Done')).getAllByRole('button', {name: /^d\d+$/})).toHaveLength(30);
    });
});
