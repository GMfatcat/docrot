import express from 'express';

/** serve starts the fixture's HTTP API. */
export function serve(): void {
  const app = express();
  app.get('/js/items', (_req, res) => res.json([]));
  app.post('/js/items', (_req, res) => res.status(201).end());
  const token = process.env.FIXTURE_TOKEN || 'none';
  app.listen(3000, () => console.log(token));
}
