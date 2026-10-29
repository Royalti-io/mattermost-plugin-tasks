import {userApi} from 'api';
import type {MMUser} from 'api';
import {useEffect, useState} from 'react';
import {useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

// Usernames learned from the API, for people the host app has not loaded into its store.
const known = new Map<string, string>();
const pending = new Set<string>();
const listeners = new Set<() => void>();

export function rememberUsers(users: MMUser[]): void {
    let changed = false;
    for (const u of users) {
        if (known.get(u.id) !== u.username) {
            known.set(u.id, u.username);
            changed = true;
        }
    }
    if (changed) {
        listeners.forEach((fn) => fn());
    }
}

// Test hook: forget everything learned so far.
export function resetUserNames(): void {
    known.clear();
    pending.clear();
}

// Returns a function that names a user id: the host store first, then names fetched
// on demand, then a short id as a last resort. Unknown ids are fetched once.
export function useUserNames(ids: string[]): (id: string) => string {
    const profiles = useSelector((s: GlobalState) => s.entities.users.profiles);
    const [, bump] = useState(0);

    useEffect(() => {
        const fn = () => bump((n) => n + 1);
        listeners.add(fn);
        return () => {
            listeners.delete(fn);
        };
    }, []);

    const missing = Array.from(new Set(ids)).filter((id) => !profiles[id] && !known.has(id) && !pending.has(id));
    const key = missing.join(',');

    useEffect(() => {
        if (!key) {
            return;
        }

        // Several components can ask for the same id in one render pass; whichever
        // effect runs first claims it, so re-check here instead of trusting render time.
        const wanted = key.split(',').filter((id) => !pending.has(id) && !known.has(id));
        if (!wanted.length) {
            return;
        }
        wanted.forEach((id) => pending.add(id));
        userApi.byIds(wanted).then(rememberUsers).catch(() => {
            // Names are cosmetic; a failed lookup leaves the short id showing.
        }).finally(() => wanted.forEach((id) => pending.delete(id)));
    }, [key]);

    return (id: string) => profiles[id]?.username || known.get(id) || id.slice(0, 6);
}
