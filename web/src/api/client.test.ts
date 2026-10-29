import { afterEach, describe, expect, it, vi } from 'vitest';
import { adminRequest, ApiError } from './client';

afterEach(() => {
  vi.unstubAllGlobals();
});

function mockFetch(response: Response) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
  return fetch as unknown as ReturnType<typeof vi.fn>;
}

describe('adminRequest', () => {
  it('sends the token as a Bearer Authorization header', async () => {
    const fetchMock = mockFetch(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    await adminRequest('secret-token', '/admin/projects');
    const [, init] = fetchMock.mock.calls[0];
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer secret-token');
  });

  it('parses the backend error shape on failure', async () => {
    mockFetch(
      new Response(
        JSON.stringify({ error: { message: 'invalid admin token', type: 'unauthorized' } }),
        { status: 401 },
      ),
    );
    await expect(adminRequest('bad-token', '/admin/projects')).rejects.toMatchObject({
      message: 'invalid admin token',
      status: 401,
    });
  });

  it('exposes isAuthError for a 401', async () => {
    mockFetch(new Response(JSON.stringify({ error: { message: 'x', type: 'unauthorized' } }), { status: 401 }));
    try {
      await adminRequest('bad-token', '/admin/projects');
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).isAuthError).toBe(true);
    }
  });

  it('falls back to statusText when the error body is not JSON', async () => {
    mockFetch(new Response('not json', { status: 500, statusText: 'Internal Server Error' }));
    await expect(adminRequest('t', '/admin/projects')).rejects.toMatchObject({
      message: 'Internal Server Error',
      status: 500,
    });
  });

  it('returns undefined for a 204 No Content response', async () => {
    mockFetch(new Response(null, { status: 204 }));
    await expect(adminRequest('t', '/admin/projects/1/cache', { method: 'DELETE' })).resolves.toBeUndefined();
  });
});
