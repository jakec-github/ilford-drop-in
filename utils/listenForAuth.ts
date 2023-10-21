import http from 'node:http';
import { URL } from 'node:url';

import GoogleApis from 'googleapis';

export const listenForAuth = (
  client: GoogleApis.Auth.OAuth2Client,
  scopes: string[],
  authPort: number,
) =>
  new Promise<string>((resolve) => {
    console.log('Please follow the link below to authenticate with Google');
    console.log(
      client.generateAuthUrl({
        scope: scopes,
      }),
    );
    const server = http
      .createServer((request, response) => {
        if (typeof request.url !== 'string') {
          response.writeHead(400, { 'Content-Type': 'text/plain' });
          response.end();
          return;
        }

        // Second argument just makes the url valid and is arbitrary
        const { searchParams } = new URL(request.url, 'http://localhost');
        const code = searchParams.get('code');

        if (code === null) {
          response.writeHead(400, { 'Content-Type': 'text/plain' });
          response.end();
          return;
        }

        response.writeHead(200, { 'Content-Type': 'text/plain' });
        response.end();
        server.close();

        resolve(code);
      })
      .listen(authPort);
  });
