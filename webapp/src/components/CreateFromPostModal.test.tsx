/**
 * @jest-environment jsdom
 */
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {api} from 'api';
import {setStore} from 'bridge';
import React from 'react';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import CreateFromPostModal, {openCreateFromPost} from './CreateFromPostModal';

jest.mock('api', () => ({api: {createTask: jest.fn()}}));

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn({entities: {users: {currentUserId: 'u1'}}}),
}));

const mocked = api as jest.Mocked<typeof api>;

beforeEach(() => {
    jest.resetAllMocks();
    setStore({
        getState: () => ({
            entities: {posts: {posts: {p1: {id: 'p1', channel_id: 'c1', message: '## Fix the login\nusers cannot sign in'}}}},
        }),
    } as unknown as Store<GlobalState>);
});

afterEach(() => {
    // Close any dialog left open so tests stay independent.
    act(() => {
        fireEvent.keyDown(document, {key: 'Escape'});
    });
});

describe('CreateFromPostModal', () => {
    it('renders nothing until opened', () => {
        render(<CreateFromPostModal/>);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('prefills the title from the message and creates the task linked to the post', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<CreateFromPostModal/>);

        act(() => openCreateFromPost('p1'));
        const title = (await screen.findByLabelText('Title')) as HTMLInputElement;
        expect(title.value).toBe('Fix the login');

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

    it('lets the user opt out of assignment and the thread reply', async () => {
        mocked.createTask.mockResolvedValue({} as never);
        render(<CreateFromPostModal/>);
        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');

        fireEvent.click(screen.getByLabelText('Assign to me'));
        fireEvent.click(screen.getByLabelText('Reply in the thread'));
        fireEvent.click(screen.getByText('Create task'));

        await waitFor(() => expect(mocked.createTask).toHaveBeenCalled());
        expect(mocked.createTask.mock.calls[0][0]).toMatchObject({assignees: [], notify_thread: false});
    });

    it('keeps the dialog open and shows the error when creation fails', async () => {
        mocked.createTask.mockRejectedValue(new Error('you cannot create tasks in this channel'));
        render(<CreateFromPostModal/>);
        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');

        fireEvent.click(screen.getByText('Create task'));

        expect((await screen.findByRole('alert')).textContent).toBe('you cannot create tasks in this channel');
        expect(screen.getByRole('dialog')).toBeTruthy();
    });

    it('closes on Cancel and on Escape', async () => {
        render(<CreateFromPostModal/>);
        act(() => openCreateFromPost('p1'));
        fireEvent.click(await screen.findByText('Cancel'));
        expect(screen.queryByRole('dialog')).toBeNull();

        act(() => openCreateFromPost('p1'));
        await screen.findByRole('dialog');
        fireEvent.keyDown(document, {key: 'Escape'});
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('ignores an unknown post id', () => {
        render(<CreateFromPostModal/>);
        act(() => openCreateFromPost('missing'));
        expect(screen.queryByRole('dialog')).toBeNull();
    });
});
