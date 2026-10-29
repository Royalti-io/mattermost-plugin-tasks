/**
 * @jest-environment jsdom
 */
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {api, userApi} from 'api';
import React from 'react';
import type {Task} from 'taskTypes';

import TaskDetail from './TaskDetail';

jest.mock('api', () => ({
    api: {
        updateTask: jest.fn(),
        moveTask: jest.fn(),
        deleteTask: jest.fn(),
        addChecklistItem: jest.fn(),
        updateChecklistItem: jest.fn(),
        deleteChecklistItem: jest.fn(),
    },
    userApi: {autocomplete: jest.fn(), byIds: jest.fn()},
}));

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn({
        entities: {
            users: {profiles: {u1: {username: 'ada'}, u2: {username: 'roy'}}},
            teams: {currentTeamId: 't1'},
        },
    }),
}));

const mocked = api as jest.Mocked<typeof api>;
const users = userApi as jest.Mocked<typeof userApi>;

const task: Task = {
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
    assignees: ['u1'],
    labels: [],
    checklist: [],
};

function setup() {
    const replace = jest.fn();
    const reload = jest.fn();
    const onError = jest.fn();
    render(
        <TaskDetail
            task={task}
            currentUserId='u1'
            replace={replace}
            reload={reload}
            onError={onError}
        />,
    );
    return {replace, reload, onError};
}

beforeEach(() => {
    jest.resetAllMocks();
    users.autocomplete.mockResolvedValue([{id: 'u2', username: 'roy'}]);
    users.byIds.mockResolvedValue([]);
});

describe('TaskDetail assignees', () => {
    it('adds another person: updates at once, then saves the full set', async () => {
        mocked.updateTask.mockResolvedValue({...task, assignees: ['u1', 'u2']});
        const {replace} = setup();

        const input = screen.getByLabelText('Add assignee');
        fireEvent.focus(input);
        fireEvent.mouseDown(await screen.findByText('@roy'));

        expect(replace).toHaveBeenCalledWith(expect.objectContaining({assignees: ['u1', 'u2']}));
        await waitFor(() => expect(mocked.updateTask).toHaveBeenCalledWith('t1', {assignees: ['u1', 'u2']}));
    });

    it('removes an assignee', async () => {
        mocked.updateTask.mockResolvedValue({...task, assignees: []});
        setup();

        fireEvent.click(screen.getByLabelText('Remove @ada'));

        await waitFor(() => expect(mocked.updateTask).toHaveBeenCalledWith('t1', {assignees: []}));
    });

    it('reports the server error and reloads when the change is refused', async () => {
        mocked.updateTask.mockRejectedValue(new Error('assignee x is not a member of the channel'));
        const {onError, reload} = setup();

        fireEvent.click(screen.getByLabelText('Remove @ada'));

        await waitFor(() => expect(onError).toHaveBeenCalledWith('assignee x is not a member of the channel'));
        expect(reload).toHaveBeenCalled();
    });
});
