/**
 * @jest-environment jsdom
 */
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {userApi} from 'api';
import React from 'react';
import {resetUserNames} from 'userNames';

import AssigneePicker from './AssigneePicker';

jest.mock('api', () => ({
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

const mocked = userApi as jest.Mocked<typeof userApi>;

const roy = {id: 'u2', username: 'roy', first_name: 'Roy', last_name: 'Lee'};
const bea = {id: 'u3', username: 'bea'};

function setup(over: Partial<React.ComponentProps<typeof AssigneePicker>> = {}) {
    const onChange = jest.fn();
    render(
        <AssigneePicker
            channelId='c1'
            assignees={[]}
            currentUserId='u1'
            onChange={onChange}
            {...over}
        />,
    );
    return {onChange, input: screen.getByLabelText('Add assignee') as HTMLInputElement};
}

beforeEach(() => {
    jest.resetAllMocks();
    resetUserNames();
    mocked.autocomplete.mockResolvedValue([roy, bea]);
    mocked.byIds.mockResolvedValue([]);
});

describe('AssigneePicker', () => {
    it('shows assignees as chips and removes one', () => {
        const {onChange} = setup({assignees: ['u1', 'u2']});
        expect(screen.getByText('@ada')).toBeTruthy();
        expect(screen.getByText('@roy')).toBeTruthy();

        fireEvent.click(screen.getByLabelText('Remove @ada'));
        expect(onChange).toHaveBeenCalledWith(['u2']);
    });

    it('searches the task channel and team as the user types', async () => {
        const {input} = setup();
        fireEvent.focus(input);
        fireEvent.change(input, {target: {value: 'ro'}});

        await waitFor(() => expect(mocked.autocomplete).toHaveBeenLastCalledWith('ro', 'c1', 't1'));
        expect(await screen.findByText('Roy Lee (@roy)')).toBeTruthy();
        expect(screen.getByText('@bea')).toBeTruthy();
    });

    it('adds the person you click', async () => {
        const {input, onChange} = setup({assignees: ['u1']});
        fireEvent.focus(input);
        fireEvent.mouseDown(await screen.findByText('Roy Lee (@roy)'));

        expect(onChange).toHaveBeenCalledWith(['u1', 'u2']);
        expect(input.value).toBe('');
    });

    it('does not offer people who are already assigned', async () => {
        const {input} = setup({assignees: ['u2']});
        fireEvent.focus(input);

        await screen.findByText('@bea');
        expect(screen.queryByText('Roy Lee (@roy)')).toBeNull();
    });

    it('picks with the keyboard: arrows move, Enter adds', async () => {
        const {input, onChange} = setup();
        fireEvent.focus(input);
        await screen.findByText('@bea');

        fireEvent.keyDown(input, {key: 'ArrowDown'});
        fireEvent.keyDown(input, {key: 'Enter'});
        expect(onChange).toHaveBeenCalledWith(['u3']);
    });

    it('closes the list on Escape without adding anyone', async () => {
        const {input, onChange} = setup();
        fireEvent.focus(input);
        await screen.findByText('@bea');

        fireEvent.keyDown(input, {key: 'Escape'});
        expect(screen.queryByRole('listbox')).toBeNull();
        expect(onChange).not.toHaveBeenCalled();
    });

    it('offers "Assign to me" only until you are assigned', () => {
        const {onChange} = setup({assignees: ['u2']});
        fireEvent.click(screen.getByText('Assign to me'));
        expect(onChange).toHaveBeenCalledWith(['u2', 'u1']);
    });

    it('hides "Assign to me" when you are already assigned', () => {
        setup({assignees: ['u1']});
        expect(screen.queryByText('Assign to me')).toBeNull();
    });

    it('says so when the search fails', async () => {
        mocked.autocomplete.mockRejectedValue(new Error('down'));
        const {input} = setup();
        fireEvent.focus(input);
        expect(await screen.findByText('Could not search people')).toBeTruthy();
    });

    it('stops at the assignee limit', () => {
        const many = Array.from({length: 20}, (_, i) => `x${i}`);
        setup({assignees: many});
        expect((screen.getByLabelText('Add assignee') as HTMLInputElement).disabled).toBe(true);
        expect((screen.getByText('Assign to me') as HTMLButtonElement).disabled).toBe(true);
    });
});
