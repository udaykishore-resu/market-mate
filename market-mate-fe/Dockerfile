FROM node:22-alpine AS build

WORKDIR /src
COPY package*.json ./
RUN npm ci

COPY . .
# Empty base URL = same-origin; nginx below proxies /api to the backend.
ENV VITE_API_BASE_URL=""
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
