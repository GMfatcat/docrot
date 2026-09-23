/**
 * Client talks to the fixture server.
 */
export class Client {
  constructor(private base: string) {}

  /** fetchAll lists every item. */
  async fetchAll(limit = 10): Promise<string[]> {
    return [this.base, String(limit)];
  }

  get size(): number {
    return 0;
  }
}

/** useItems is the hook the UI calls. */
export function useItems(id: string): string {
  return id;
}

export const VERSION = '0.1.0';
