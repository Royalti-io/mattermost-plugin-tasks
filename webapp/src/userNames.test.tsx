/**
 * @jest-environment jsdom
 */
import {render, screen, waitFor} from '@testing-library/react';
import {userApi} from 'api';
import React from 'react';

import {resetUserNames, useUserNames} from './userNames';

jest.mock('api', () => ({
    userApi: {autocomplete: jest.fn(), byIds: jest.fn()},
}));

jest.mock('react-redux', () => ({
    useSelector: (fn: (s: unknown) => unknown) => fn({entities: {users: {profiles: {u1: {username: 'ada'}}}}}),
}));

const mocked = userApi as jest.Mocked<typeof userApi>;

function Names({ids}: {ids: string[]}) {
    const name = useUserNames(ids);
    return <div data-testid='names'>{ids.map(name).join(',')}</div>;
}

beforeEach(() => {
    jest.resetAllMocks();
    resetUserNames();
});

describe('useUserNames', () => {
    it('uses the host store without calling the API', () => {
        render(<Names ids={['u1']}/>);
        expect(screen.getByTestId('names').textContent).toBe('ada');
        expect(mocked.byIds).not.toHaveBeenCalled();
    });

    it('shows a short id first, then the fetched username', async () => {
        mocked.byIds.mockResolvedValue([{id: 'u9abcdefghijk', username: 'roy'}]);
        render(<Names ids={['u9abcdefghijk']}/>);

        expect(screen.getByTestId('names').textContent).toBe('u9abcd');
        await waitFor(() => expect(screen.getByTestId('names').textContent).toBe('roy'));
        expect(mocked.byIds).toHaveBeenCalledWith(['u9abcdefghijk']);
    });

    it('fetches each unknown id only once, even across components', async () => {
        mocked.byIds.mockResolvedValue([{id: 'u8xxxxxxxx', username: 'bea'}]);
        render(<><Names ids={['u8xxxxxxxx']}/><Names ids={['u8xxxxxxxx', 'u8xxxxxxxx']}/></>);

        await waitFor(() => expect(screen.getAllByTestId('names')[0].textContent).toBe('bea'));
        expect(mocked.byIds).toHaveBeenCalledTimes(1);
    });

    it('keeps the short id when the lookup fails', async () => {
        mocked.byIds.mockRejectedValue(new Error('down'));
        render(<Names ids={['u7yyyyyyyy']}/>);
        await waitFor(() => expect(mocked.byIds).toHaveBeenCalled());
        expect(screen.getByTestId('names').textContent).toBe('u7yyyy');
    });
});
