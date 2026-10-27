ARG BASE_IMAGE=ruby:3.3.12-slim-bookworm@sha256:df209d82bb55aeb26b9b08922ee22c15f19b3458455690132dc07a5c52dc1c99
FROM ${BASE_IMAGE}
WORKDIR /app
COPY Gemfile Gemfile.lock ./
ENV BUNDLE_FROZEN=true BUNDLE_PATH=/usr/local/bundle RACK_ENV=production
RUN bundle install && bundle clean --force
COPY app.rb ./
EXPOSE 8080
CMD ["ruby", "app.rb"]
