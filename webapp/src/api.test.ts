/**
 * @jest-environment jsdom
 */
import {ApiError, api, buildQuery} from 'api';

const fetchMock = jest.fn();

beforeEach(() => {
    fetchMock.mockReset();
    (global as unknown as {fetch: unknown}).fetch = fetchMock;
    document.cookie = 'MMCSRF=tok%20en123';
});

const reply = (status: number, body?: unknown) => ({
    ok: status >= 200 && status < 300,
    status,
    text: async () => (body === undefined ? '' : JSON.stringify(body)),
});

describe('buildQuery', () => {
    it('omits empty values and encodes the rest', () => {
        expect(buildQuery({a: 'x y', b: undefined, c: '', d: 0})).toBe('?a=x+y&d=0');
        expect(buildQuery({})).toBe('');
    });
});

describe('api client', () => {
    it('lists tasks with a GET and no CSRF header', async () => {
        fetchMock.mockResolvedValue(reply(200, {tasks: [{id: 't1'}]}));
        const tasks = await api.listTasks({channel_id: 'c1', status: 'todo,done'});

        expect(tasks).toEqual([{id: 't1'}]);
        const [url, init] = fetchMock.mock.calls[0];
        expect(url).toBe('/plugins/com.royalti.tasks/api/v1/tasks?channel_id=c1&status=todo%2Cdone');
        expect(init.method).toBe('GET');
        expect(init.headers['X-CSRF-Token']).toBeUndefined();
        expect(init.credentials).toBe('same-origin');
    });

    it('sends the CSRF token and JSON body on writes', async () => {
        fetchMock.mockResolvedValue(reply(201, {id: 't2'}));
        await api.createTask({channel_id: 'c1', title: 'x'});

        const [, init] = fetchMock.mock.calls[0];
        expect(init.method).toBe('POST');
        expect(init.headers['X-CSRF-Token']).toBe('tok en123');
        expect(init.headers['Content-Type']).toBe('application/json');
        expect(JSON.parse(init.body)).toEqual({channel_id: 'c1', title: 'x'});
    });

    it('sends an explicit null to clear a due date', async () => {
        fetchMock.mockResolvedValue(reply(200, {id: 't1'}));
        await api.updateTask('t1', {due_at: null});
        expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({due_at: null});
    });

    it('treats 204 as success with no body', async () => {
        fetchMock.mockResolvedValue(reply(204));
        await expect(api.deleteTask('t1')).resolves.toBeUndefined();
    });

    it('surfaces the server error message', async () => {
        fetchMock.mockResolvedValue(reply(400, {error: 'title is required'}));
        await expect(api.createTask({channel_id: 'c', title: ''})).rejects.toMatchObject({
            message: 'title is required',
            status: 400,
        });
    });

    it('falls back to a generic message when the error body is not JSON', async () => {
        fetchMock.mockResolvedValue({ok: false, status: 502, text: async () => '<html>bad gateway</html>'});
        const err = await api.listTasks({}).catch((e) => e);
        expect(err).toBeInstanceOf(ApiError);
        expect(err.message).toBe('Request failed (502)');
    });
});
