import {userApi} from 'api';
import type {MMUser} from 'api';
import React, {useEffect, useRef, useState} from 'react';
import {useSelector} from 'react-redux';
import {rememberUsers, useUserNames} from 'userNames';

import type {GlobalState} from '@mattermost/types/store';

type Props = {
    channelId: string;
    assignees: string[];
    currentUserId: string;
    onChange: (next: string[]) => void;
};

const MAX_ASSIGNEES = 20;

function display(u: MMUser): string {
    const full = [u.first_name, u.last_name].filter(Boolean).join(' ').trim();
    return full ? `${full} (@${u.username})` : `@${u.username}`;
}

// Chips for the current assignees plus an autocomplete box limited to members of the
// task's channel, since the server only accepts channel members as assignees.
export default function AssigneePicker({channelId, assignees, currentUserId, onChange}: Props) {
    const teamId = useSelector((s: GlobalState) => s.entities.teams?.currentTeamId);
    const nameOf = useUserNames(assignees);
    const [text, setText] = useState('');
    const [options, setOptions] = useState<MMUser[]>([]);
    const [active, setActive] = useState(0);
    const [open, setOpen] = useState(false);
    const [failed, setFailed] = useState(false);
    const seq = useRef(0);
    const listId = useRef(`rt-assignee-list-${Math.random().toString(36).slice(2, 8)}`).current;

    const assigned = new Set(assignees);
    const full = assignees.length >= MAX_ASSIGNEES;

    useEffect(() => {
        if (!open) {
            return undefined;
        }
        const mine = ++seq.current;
        const t = setTimeout(async () => {
            try {
                const users = await userApi.autocomplete(text.trim(), channelId, teamId);
                if (mine === seq.current) {
                    rememberUsers(users);
                    setOptions(users.filter((u) => !assigned.has(u.id)));
                    setActive(0);
                    setFailed(false);
                }
            } catch {
                if (mine === seq.current) {
                    setOptions([]);
                    setFailed(true);
                }
            }
        }, text ? 200 : 0);
        return () => clearTimeout(t);

        // `assigned` is derived from `assignees`; depend on its content, not identity.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [text, open, channelId, teamId, assignees.join(',')]);

    const add = (u: MMUser) => {
        if (assigned.has(u.id) || full) {
            return;
        }
        rememberUsers([u]);
        onChange([...assignees, u.id]);
        setText('');
        setOpen(false);
    };

    const remove = (id: string) => onChange(assignees.filter((a) => a !== id));

    return (
        <div className='rt-assignees'>
            <div className='rt-assignees__chips'>
                {assignees.map((id) => (
                    <span
                        key={id}
                        className='rt-chip rt-chip--user'
                    >
                        {'@' + nameOf(id)}
                        <button
                            type='button'
                            className='rt-chip__x'
                            aria-label={`Remove @${nameOf(id)}`}
                            onClick={() => remove(id)}
                        >{'×'}</button>
                    </span>
                ))}
                {assigned.has(currentUserId) ? null : (
                    <button
                        type='button'
                        className='rt-link'
                        onClick={() => onChange([...assignees, currentUserId])}
                        disabled={full}
                    >{'Assign to me'}</button>
                )}
            </div>
            <div className='rt-assignees__box'>
                <input
                    className='rt-input'
                    role='combobox'
                    aria-expanded={open && options.length > 0}
                    aria-controls={listId}
                    aria-autocomplete='list'
                    aria-label='Add assignee'
                    placeholder={full ? 'Assignee limit reached' : 'Add assignee by name'}
                    disabled={full}
                    value={text}
                    onFocus={() => setOpen(true)}
                    onBlur={() => setTimeout(() => setOpen(false), 150)}
                    onChange={(e) => {
                        setText(e.target.value);
                        setOpen(true);
                    }}
                    onKeyDown={(e) => {
                        if (e.key === 'ArrowDown' && options.length) {
                            e.preventDefault();
                            setActive((i) => Math.min(i + 1, options.length - 1));
                        } else if (e.key === 'ArrowUp' && options.length) {
                            e.preventDefault();
                            setActive((i) => Math.max(i - 1, 0));
                        } else if (e.key === 'Enter' && open && options[active]) {
                            e.preventDefault();
                            add(options[active]);
                        } else if (e.key === 'Escape') {
                            setOpen(false);
                        }
                    }}
                />
                {open && (options.length > 0 || failed) ? (
                    <ul
                        id={listId}
                        className='rt-assignees__list'
                        role='listbox'
                    >
                        {options.map((u, i) => (
                            <li
                                key={u.id}
                                role='option'
                                aria-selected={i === active}
                                className={`rt-assignees__opt${i === active ? ' rt-assignees__opt--on' : ''}`}

                                // mousedown fires before the input's blur closes the list
                                onMouseDown={(e) => {
                                    e.preventDefault();
                                    add(u);
                                }}
                            >{display(u)}</li>
                        ))}
                        {failed ? <li className='rt-assignees__note'>{'Could not search people'}</li> : null}
                    </ul>
                ) : null}
            </div>
        </div>
    );
}
