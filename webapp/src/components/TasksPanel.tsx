import {api} from 'api';
import {useTasks} from 'hooks';
import React, {useEffect, useState} from 'react';
import {useSelector} from 'react-redux';
import {OPEN_STATUSES} from 'taskTypes';

import type {GlobalState} from '@mattermost/types/store';

import TaskItem from './TaskItem';

type Scope = 'channel' | 'mine' | 'all';
type StatusFilter = 'open' | 'done' | 'all';

const STATUS_QUERY: Record<StatusFilter, string | undefined> = {
    open: OPEN_STATUSES.join(','),
    done: 'done',
    all: undefined,
};

export default function TasksPanel() {
    const currentUserId = useSelector((s: GlobalState) => s.entities.users.currentUserId);
    const channelId = useSelector((s: GlobalState) => s.entities.channels.currentChannelId);
    const channels = useSelector((s: GlobalState) => s.entities.channels.channels);
    const profiles = useSelector((s: GlobalState) => s.entities.users.profiles);

    const [scope, setScope] = useState<Scope>('channel');
    const [statusFilter, setStatusFilter] = useState<StatusFilter>('open');
    const [search, setSearch] = useState('');
    const [query, setQuery] = useState('');
    const [title, setTitle] = useState('');
    const [adding, setAdding] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Debounce the search box so each keystroke does not hit the server.
    useEffect(() => {
        const t = setTimeout(() => setQuery(search.trim()), 300);
        return () => clearTimeout(t);
    }, [search]);

    const {tasks, loading, error: loadError, reload, replace} = useTasks({
        channel_id: scope === 'channel' ? channelId : undefined,
        assignee: scope === 'mine' ? 'me' : undefined,
        status: STATUS_QUERY[statusFilter],
        q: query || undefined,
        limit: 200,
    });

    const channelLabel = (id: string) => {
        const ch = channels[id];
        return ch ? (ch.display_name || ch.name) : undefined;
    };
    const userName = (id: string) => profiles[id]?.username || id.slice(0, 6);

    const addTask = async () => {
        const t = title.trim();
        if (!t || adding) {
            return;
        }
        setAdding(true);
        setError(null);
        try {
            await api.createTask({
                channel_id: channelId,
                title: t,
                assignees: scope === 'mine' ? [currentUserId] : undefined,
            });
            setTitle('');
            reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Could not create the task');
        } finally {
            setAdding(false);
        }
    };

    const shownError = error || loadError;
    const where = channelLabel(channelId) ? `~${channelLabel(channelId)}` : 'this channel';

    return (
        <div className='rt-panel'>
            <div
                className='rt-segment'
                role='tablist'
            >
                {([['channel', 'This channel'], ['mine', 'Assigned to me'], ['all', 'All channels']] as Array<[Scope, string]>).map(([key, label]) => (
                    <button
                        key={key}
                        type='button'
                        role='tab'
                        aria-selected={scope === key}
                        className={`rt-segment__btn${scope === key ? ' rt-segment__btn--on' : ''}`}
                        onClick={() => setScope(key)}
                    >{label}</button>
                ))}
            </div>
            <div className='rt-filters'>
                <input
                    className='rt-input'
                    type='search'
                    value={search}
                    placeholder='Search tasks'
                    aria-label='Search tasks'
                    onChange={(e) => setSearch(e.target.value)}
                />
                <select
                    className='rt-input'
                    value={statusFilter}
                    aria-label='Status filter'
                    onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                >
                    <option value='open'>{'Open'}</option>
                    <option value='done'>{'Done'}</option>
                    <option value='all'>{'All'}</option>
                </select>
            </div>
            <div className='rt-add'>
                <input
                    className='rt-input'
                    value={title}
                    maxLength={200}
                    placeholder={`Add a task to ${where}`}
                    aria-label='New task title'
                    onChange={(e) => setTitle(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                            addTask();
                        }
                    }}
                />
                <button
                    type='button'
                    className='rt-btn'
                    disabled={!title.trim() || adding}
                    onClick={addTask}
                >{'Add'}</button>
            </div>
            {shownError ? (
                <div
                    className='rt-error'
                    role='alert'
                >{shownError}</div>
            ) : null}
            {loading && !tasks.length ? <div className='rt-empty'>{'Loading…'}</div> : null}
            {!loading && !tasks.length && !shownError ? (
                <div className='rt-empty'>{statusFilter === 'done' ? 'No completed tasks.' : 'No tasks here yet.'}</div>
            ) : null}
            <ul className='rt-list'>
                {tasks.map((task) => (
                    <TaskItem
                        key={task.id}
                        task={task}
                        channelName={scope === 'channel' ? undefined : channelLabel(task.channel_id)}
                        currentUserId={currentUserId}
                        userName={userName}
                        replace={replace}
                        reload={reload}
                        onError={setError}
                    />
                ))}
            </ul>
        </div>
    );
}
