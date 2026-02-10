FROM ruby:3.2-slim

# Install MySQL client libraries for mysql2 gem
# Using Debian-based image instead of Alpine because Alpine ships MariaDB's
# libmysqlclient which doesn't support ssl_mode (needed for self-signed certs)
RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
      build-essential \
      default-libmysqlclient-dev && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY Gemfile ./
RUN bundle install

COPY app.rb ./

CMD ["ruby", "app.rb"]
