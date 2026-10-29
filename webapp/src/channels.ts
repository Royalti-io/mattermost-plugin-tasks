import {userApi} from 'api';
import {useEffect, useState} from 'react';
import {useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

export type ChannelChoice = {id: string; label: string; name: string};

type ChannelLike = {id: string; type: string; name: string; display_name: string; delete_at: number};

function toChoice(c: ChannelLike): ChannelChoice {
    const name = c.display_name || c.name;
    return {id: c.id, label: '~' + name, name};
}

// Public and private channels, alphabetical. Archived ones are left out.
function pickable(channels: ChannelLike[]): ChannelChoice[] {
    return channels.
        filter((c) => (c.type === 'O' || c.type === 'P') && c.delete_at === 0).
        sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name)).
        map(toChoice);
}

// Channels the user can put a task in: the public and private channels they belong to,
// plus `extraId` (usually the open channel) even when it is a direct or group message.
//
// Mattermost only loads channels for the team in the URL. A page opened directly
// (such as /tasks-board) has no team, so the store is empty; then the user's channels
// are fetched across all teams instead.
export function useChannelChoices(extraId?: string): ChannelChoice[] {
    const channels = useSelector((s: GlobalState) => s.entities.channels.channels);
    const myMembers = useSelector((s: GlobalState) => s.entities.channels.myMembers);
    const [fetched, setFetched] = useState<ChannelChoice[]>([]);

    const fromStore = pickable(Object.values(channels).filter((c) => myMembers[c.id]));
    const needsFetch = fromStore.length === 0;

    useEffect(() => {
        if (!needsFetch) {
            return undefined;
        }
        let cancelled = false;
        Promise.resolve().
            then(() => userApi.myChannels()).
            then((list) => {
                if (!cancelled) {
                    setFetched(pickable(list));
                }
            }).
            catch(() => {
                // Leave the picker empty; the page already says there is nothing to show.
            });
        return () => {
            cancelled = true;
        };
    }, [needsFetch]);

    const choices = needsFetch ? fetched : fromStore;

    const extra = extraId ? channels[extraId] : undefined;
    if (extra && myMembers[extra.id] && extra.delete_at === 0 && !choices.some((c) => c.id === extra.id)) {
        let label = 'This conversation';
        if (extra.type === 'D') {
            label = 'This direct message';
        } else if (extra.type === 'G') {
            label = 'This group message';
        }
        return [{id: extra.id, label, name: label}, ...choices];
    }
    return choices;
}
