/**
 * @jest-environment jsdom
 */
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {api} from 'api';
import React from 'react';
import {notifyTasksChanged} from 'taskEvents';
import type {Task} from 'taskTypes';

import TasksPanel from './TasksPanel';

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
    userApi: {autocomplete: jest.fn().mockResolvedValue([]), byIds: jest.fn().mockResolvedValue([])},
}));

const state = {
    entities: {
        users: {currentUserId: 'u1', profiles: {u1: {username: 'ada'}, u2: {username: 'roy'}}},
        channels: {
            currentChannelId: 'c1',
            channels: {c1: {display_name: 'General', name: 'general'}, c2: {display_name: 'Ops', name: 'ops'}},
        },
    },
};

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn(state),
}));

const mocked = api as jest.Mocked<typeof api>;

const task = (over: Partial<Task> = {}): Task => ({
    id: 't1',
    channel_id: 'c1',
    title: 'Write docs',
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

beforeEach(() => {
    jest.resetAllMocks();
    mocked.listTasks.mockResolvedValue([task()]);
});

describe('TasksPanel', () => {
    it('lists open tasks for the current channel', async () => {
        render(<TasksPanel/>);

        expect(await screen.findByText('Write docs')).toBeTruthy();
        expect(mocked.listTasks).toHaveBeenCalledWith(expect.objectContaining({
            channel_id: 'c1',
            status: 'backlog,todo,in_progress,blocked',
        }));
    });

    it('shows an empty state', async () => {
        mocked.listTasks.mockResolvedValue([]);
        render(<TasksPanel/>);
        expect(await screen.findByText('No tasks here yet.')).toBeTruthy();
    });

    it('shows the load error', async () => {
        mocked.listTasks.mockRejectedValue(new Error('boom'));
        render(<TasksPanel/>);
        expect((await screen.findByRole('alert')).textContent).toBe('boom');
    });

    it('creates a task from the quick-add box and clears it', async () => {
        mocked.createTask.mockResolvedValue(task({id: 't2', title: 'New one'}));
        render(<TasksPanel/>);
        await screen.findByText('Write docs');

        const input = screen.getByLabelText('New task title') as HTMLInputElement;
        fireEvent.change(input, {target: {value: '  New one  '}});
        fireEvent.keyDown(input, {key: 'Enter'});

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalledWith({
            channel_id: 'c1',
            title: 'New one',
            assignees: undefined,
        }));
        await waitFor(() => expect(input.value).toBe(''));
    });

    it('disables Add for a blank title', async () => {
        render(<TasksPanel/>);
        await screen.findByText('Write docs');
        expect((screen.getByText('Add') as HTMLButtonElement).disabled).toBe(true);
    });

    it('completes a task inline and marks it done at once', async () => {
        mocked.moveTask.mockResolvedValue(task({status: 'done', completed_at: 5}));
        render(<TasksPanel/>);
        await screen.findByText('Write docs');

        fireEvent.click(screen.getByLabelText('Mark as done'));

        await waitFor(() => expect(mocked.moveTask).toHaveBeenCalledWith('t1', 'done'));
        expect(await screen.findByLabelText('Mark as not done')).toBeTruthy();
    });

    it('switches to "Assigned to me" and drops the channel filter', async () => {
        render(<TasksPanel/>);
        await screen.findByText('Write docs');

        fireEvent.click(screen.getByText('Assigned to me'));

        await waitFor(() => {
            const last = mocked.listTasks.mock.calls[mocked.listTasks.mock.calls.length - 1][0];
            expect(last).toMatchObject({assignee: 'me'});
            expect(last).not.toHaveProperty('channel_id');
        });
    });

    it('shows channel names outside the channel scope', async () => {
        mocked.listTasks.mockResolvedValue([task({channel_id: 'c2', title: 'Rotate keys'})]);
        render(<TasksPanel/>);
        await screen.findByText('Rotate keys');
        expect(screen.queryByText('~Ops')).toBeNull();

        fireEvent.click(screen.getByText('All channels'));
        expect(await screen.findByText('~Ops')).toBeTruthy();
    });

    it('reloads when the server pushes a change', async () => {
        jest.useFakeTimers();
        try {
            render(<TasksPanel/>);
            await act(async () => {
                await Promise.resolve();
            });
            const before = mocked.listTasks.mock.calls.length;

            act(() => {
                notifyTasksChanged();
                notifyTasksChanged();
                jest.advanceTimersByTime(300);
            });

            await waitFor(() => expect(mocked.listTasks.mock.calls.length).toBe(before + 1));
        } finally {
            jest.useRealTimers();
        }
    });

    it('shows assignee names and the checklist count', async () => {
        mocked.listTasks.mockResolvedValue([task({
            assignees: ['u2'],
            checklist: [{id: 'i1', text: 'a', done: true, position: 0}, {id: 'i2', text: 'b', done: false, position: 1}],
        })]);
        render(<TasksPanel/>);
        expect(await screen.findByText('@roy')).toBeTruthy();
        expect(screen.getByText('1/2')).toBeTruthy();
    });
});
