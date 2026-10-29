/**
 * @jest-environment jsdom
 */
import {act, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {api, userApi} from 'api';
import {setStore} from 'bridge';
import React from 'react';
import type {Store} from 'redux';
import {resetUserNames} from 'userNames';

import type {GlobalState} from '@mattermost/types/store';

import TaskCreateModal, {openCreateFromPost, openNewTask} from './TaskCreateModal';

jest.mock('api', () => ({
    api: {createTask: jest.fn()},
    userApi: {autocomplete: jest.fn(), byIds: jest.fn()},
}));

const baseState = () => ({
    entities: {
        users: {currentUserId: 'u1', profiles: {u1: {username: 'ada'}, u2: {username: 'roy'}}},
        teams: {currentTeamId: 't1'},
        posts: {posts: {p1: {id: 'p1', channel_id: 'c1', message: '## Fix the login\nusers cannot sign in'}}},
        channels: {
            currentChannelId: 'c1',
            channels: {
                c1: {id: 'c1', type: 'O', display_name: 'General', name: 'general', delete_at: 0},
                c2: {id: 'c2', type: 'P', display_name: 'Ops', name: 'ops', delete_at: 0},
                c3: {id: 'c3', type: 'O', display_name: 'Not mine', name: 'nm', delete_at: 0},
                d1: {id: 'd1', type: 'D', display_name: '', name: 'u1__u2', delete_at: 0},
            },
            myMembers: {c1: {}, c2: {}, d1: {}},
        },
    },
});

let mockState = baseState();

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn(mockState),
}));

const mocked = api as jest.Mocked<typeof api>;
const users = userApi as jest.Mocked<typeof userApi>;

beforeEach(() => {
    jest.resetAllMocks();
    resetUserNames();
    mockState = baseState();
    users.autocomplete.mockResolvedValue([{id: 'u2', username: 'roy'}]);
    users.byIds.mockResolvedValue([]);
    setStore({getState: () => mockState} as unknown as Store<GlobalState>);
});

afterEach(() => {
    // Close any dialog left open so tests stay independent.
    act(() => {
        fireEvent.keyDown(document, {key: 'Escape'});
    });
});

describe('creating from a message', () => {
    it('renders nothing until opened', () => {
        render(<TaskCreateModal/>);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('prefills the title, fixes the channel, and links the task to the post', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<TaskCreateModal/>);

        act(() => openCreateFromPost('p1'));
        const title = (await screen.findByLabelText('Title')) as HTMLInputElement;
        expect(title.value).toBe('Fix the login');
        expect(screen.getByRole('dialog').getAttribute('aria-label')).toBe('Create task from message');
        expect(screen.queryByLabelText('Channel')).toBeNull();

        fireEvent.click(screen.getByText('Create task'));

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalledTimes(1));
        const arg = mocked.createTask.mock.calls[0][0];
        expect(arg).toMatchObject({
            channel_id: 'c1',
            title: 'Fix the login',
            post_id: 'p1',
            notify_thread: true,
            assignees: ['u1'],
            priority: 'normal',
        });
        expect(arg.description).toContain('users cannot sign in');
        expect(arg.description).toContain('/_redirect/pl/p1');
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    });

    it('lets the user drop the default assignee and skip the thread reply', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<TaskCreateModal/>);
        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');

        fireEvent.click(screen.getByLabelText('Remove @ada'));
        fireEvent.click(screen.getByLabelText('Reply in the thread'));
        fireEvent.click(screen.getByText('Create task'));

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalled());
        expect(mocked.createTask.mock.calls[0][0]).toMatchObject({assignees: [], notify_thread: false});
    });

    it('keeps the dialog open and shows the error when creation fails', async () => {
        mocked.createTask.mockRejectedValue(new Error('you cannot create tasks in this channel'));
        render(<TaskCreateModal/>);
        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');

        fireEvent.click(screen.getByText('Create task'));

        expect((await screen.findByRole('alert')).textContent).toBe('you cannot create tasks in this channel');
        expect(screen.getByRole('dialog')).toBeTruthy();
    });

    it('closes on Cancel and on Escape', async () => {
        render(<TaskCreateModal/>);
        act(() => openCreateFromPost('p1'));
        fireEvent.click(await screen.findByText('Cancel'));
        expect(screen.queryByRole('dialog')).toBeNull();

        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');
        fireEvent.keyDown(document, {key: 'Escape'});
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('ignores an unknown post id', () => {
        render(<TaskCreateModal/>);
        act(() => openCreateFromPost('missing'));
        expect(screen.queryByRole('dialog')).toBeNull();
    });
});

describe('creating from the sidebar "+" menu', () => {
    it('opens a blank form in the open channel, and Create waits for a title', async () => {
        render(<TaskCreateModal/>);
        act(() => openNewTask());

        const dialog = await screen.findByRole('dialog');
        expect(dialog.getAttribute('aria-label')).toBe('Create a task');
        expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('');
        expect((screen.getByLabelText('Channel') as HTMLSelectElement).value).toBe('c1');
        expect((screen.getByText('Create task') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByLabelText('Title'), {target: {value: ' Order catering '}});
        expect((screen.getByText('Create task') as HTMLButtonElement).disabled).toBe(false);
    });

    it('offers the channels you belong to, and not the ones you do not', async () => {
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        const select = (await screen.findByLabelText('Channel')) as HTMLSelectElement;

        expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['~General', '~Ops']);
    });

    it('creates a plain task: no message link, no thread reply', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        await screen.findByRole('dialog');

        fireEvent.change(screen.getByLabelText('Title'), {target: {value: 'Order catering'}});
        fireEvent.change(screen.getByLabelText('Description'), {target: {value: 'for 40 people'}});
        fireEvent.change(screen.getByLabelText('Priority'), {target: {value: 'high'}});
        fireEvent.click(screen.getByText('Create task'));

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalledTimes(1));
        const arg = mocked.createTask.mock.calls[0][0];
        expect(arg).toMatchObject({
            channel_id: 'c1',
            title: 'Order catering',
            description: 'for 40 people',
            priority: 'high',
            assignees: ['u1'],
        });
        expect(arg).not.toHaveProperty('post_id');
        expect(arg).not.toHaveProperty('notify_thread');
        expect(screen.queryByLabelText('Reply in the thread')).toBeNull();
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    });

    it('creates in the channel you pick, and resets other assignees because they may not be members', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        await screen.findByRole('dialog');

        // Add roy in General, then move to Ops: roy may not be in Ops.
        fireEvent.focus(screen.getByLabelText('Add assignee'));
        fireEvent.mouseDown(await screen.findByText('@roy'));
        expect(screen.getByText('@roy')).toBeTruthy();

        fireEvent.change(screen.getByLabelText('Channel'), {target: {value: 'c2'}});
        fireEvent.change(screen.getByLabelText('Title'), {target: {value: 'Rotate keys'}});
        fireEvent.click(screen.getByText('Create task'));

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalled());
        expect(mocked.createTask.mock.calls[0][0]).toMatchObject({channel_id: 'c2', assignees: ['u1']});
        await waitFor(() => expect(users.autocomplete).toHaveBeenCalledWith('', 'c1', 't1'));
    });

    it('starts in a direct message when that is the open conversation', async () => {
        mockState.entities.channels.currentChannelId = 'd1';
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        const select = (await screen.findByLabelText('Channel')) as HTMLSelectElement;

        expect(select.value).toBe('d1');
        expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['This direct message', '~General', '~Ops']);
    });

    it('falls back to the first channel when the open one is not usable', async () => {
        mockState.entities.channels.currentChannelId = 'c3'; // a channel you have not joined
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        expect(((await screen.findByLabelText('Channel')) as HTMLSelectElement).value).toBe('c1');
    });

    it('says so, and blocks Create, when you belong to no channel', async () => {
        mockState.entities.channels.myMembers = {} as never;
        render(<TaskCreateModal/>);
        act(() => openNewTask());

        expect(await screen.findByText('Join a channel to create tasks.')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('Title'), {target: {value: 'x'}});
        expect((screen.getByText('Create task') as HTMLButtonElement).disabled).toBe(true);
    });

    it('starts blank every time it is opened', async () => {
        render(<TaskCreateModal/>);
        act(() => openNewTask());
        fireEvent.change(await screen.findByLabelText('Title'), {target: {value: 'half typed'}});
        fireEvent.click(screen.getByText('Cancel'));

        act(() => openNewTask());
        expect(((await screen.findByLabelText('Title')) as HTMLInputElement).value).toBe('');
    });
});
