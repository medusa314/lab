FROM
Learn more about the "FROM" Dockerfile command.
 node:18-alpine
WORKDIR /app
COPY . .
RUN yarn install --production
CMD ["node", "src/index.js"]
EXPOSE 3000
