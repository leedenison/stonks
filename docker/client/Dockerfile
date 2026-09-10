# The front end image: Next.js standalone output served by node. The build stage
# mirrors the repository layout under /src so the root buf template runs
# unchanged, with buf and protoc-gen-es coming from the client's own lockfile.
FROM node:26.8.1-alpine3.24 AS build
ENV NEXT_TELEMETRY_DISABLED=1
WORKDIR /src
COPY client/package.json client/package-lock.json client/
RUN cd client && npm ci
COPY buf.yaml buf.lock buf.gen.ts.yaml ./
COPY proto/ proto/
RUN client/node_modules/.bin/buf generate --template buf.gen.ts.yaml --include-imports
COPY client/ client/
# Inlined into the bundle at build time.
ARG GOOGLE_OAUTH_CLIENT_ID
ENV NEXT_PUBLIC_GOOGLE_CLIENT_ID=$GOOGLE_OAUTH_CLIENT_ID
RUN cd client && npm run build

FROM node:26.8.1-alpine3.24
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
WORKDIR /app
COPY --from=build --chown=node:node /src/client/.next/standalone ./
COPY --from=build --chown=node:node /src/client/.next/static ./.next/static
COPY --from=build --chown=node:node /src/client/public ./public
USER node
EXPOSE 3000
CMD ["node", "server.js"]
