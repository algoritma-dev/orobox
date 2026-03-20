# Dockerfile template for OroCommerce development
ARG PHP_VERSION=8.1
ARG NODE_VERSION=18
ARG NPM_VERSION=10
ARG MEMORY_LIMIT=2048M

# Get Node from official image
FROM node:${NODE_VERSION}-alpine AS node_image

FROM php:${PHP_VERSION}-fpm-alpine

ARG PHP_VERSION
ARG NODE_VERSION
ARG NPM_VERSION
ARG MEMORY_LIMIT

# Install system dependencies
RUN apk add --no-cache \
    nginx \
    jq \
    chromium \
    bash \
    curl \
    chromium-chromedriver \
    postgresql-client \
    postgresql-dev \
    libpng-dev \
    libjpeg-turbo-dev \
    freetype-dev \
    libwebp-dev \
    libzip-dev \
    icu-dev \
    libxml2-dev \
    tidyhtml-dev \
    imap-dev \
    krb5-dev \
    openssl-dev \
    jpegoptim \
    pngquant \
    optipng \
    libjpeg-turbo \
    libpng \
    libwebp \
    freetype \
    libjpeg-turbo-utils \
    git \
    unzip \
    libstdc++ \
    libgcc \
    ca-certificates \
    linux-headers

# Install Composer
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
ENV COMPOSER_ALLOW_SUPERUSER=1

# Install Node.js from official image
COPY --from=node_image /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node_image /usr/local/bin/node /usr/local/bin/node
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && npm install -g npm@${NPM_VERSION}

# Install PHP extensions
{{ if eq .PHPVersion "8.4" -}}
RUN docker-php-source extract \
    && mkdir -p /usr/src/php/ext/imap \
    && curl -fsSL https://pecl.php.net/get/imap | tar xvz -C /usr/src/php/ext/imap --strip-components=1 \
    && docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
    && docker-php-ext-configure imap --with-kerberos --with-imap-ssl \
    && docker-php-ext-install -j$(nproc) \
        gd \
        pdo_pgsql \
        intl \
        zip \
        opcache \
        bcmath \
        soap \
        xml \
        pcntl \
        tidy \
        imap \
    && docker-php-source delete
{{- else -}}
RUN docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
    && docker-php-ext-configure imap --with-kerberos --with-imap-ssl \
    && docker-php-ext-install -j$(nproc) \
        gd \
        pdo_pgsql \
        intl \
        zip \
        opcache \
        bcmath \
        soap \
        xml \
        pcntl \
        tidy \
        imap
{{- end }}

# Install Xdebug and PCOV
RUN apk add --no-cache --virtual .build-deps $PHPIZE_DEPS \
    && pecl install xdebug pcov \
    && docker-php-ext-enable pcov \
    && docker-php-ext-enable xdebug \
    && echo "xdebug.mode=develop,debug" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.max_nesting_level=500" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.client_host=host.docker.internal" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.client_port=9003" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.discover_client_host=no" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.start_with_request=yes" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.idekey=PHPSTORM" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.log=/tmp/xdebug.log" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && echo "xdebug.connect_timeout_ms=200" >> /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled \
    && apk del .build-deps \
    && chmod -R 777 /usr/local/etc/php/conf.d

# PHP Configuration
RUN { \
        echo "memory_limit=${MEMORY_LIMIT}"; \
        echo 'upload_max_filesize=100M'; \
        echo 'post_max_size=100M'; \
        echo 'max_execution_time=600'; \
        echo 'date.timezone=UTC'; \
        echo 'short_open_tag=off'; \
        echo 'intl.error_level=0'; \
        echo "opcache.enable=0"; \
        echo "opcache.enable_cli=0"; \
        echo 'opcache.memory_consumption=256'; \
        echo 'opcache.interned_strings_buffer=16'; \
        echo 'opcache.max_accelerated_files=20000'; \
        echo 'opcache.validate_timestamps=1'; \
        echo 'opcache.revalidate_freq=0'; \
        echo 'opcache.fast_shutdown=1'; \
    } > /usr/local/etc/php/conf.d/oro-custom.ini

# Increase FPM workers for better debug experience
RUN { \
        echo '[www]'; \
        echo 'pm = dynamic'; \
        echo 'pm.max_children = 20'; \
        echo 'pm.start_servers = 5'; \
        echo 'pm.min_spare_servers = 3'; \
        echo 'pm.max_spare_servers = 10'; \
    } > /usr/local/etc/php-fpm.d/z-oro-fpm.conf

# Configure Nginx
COPY nginx.conf /etc/nginx/http.d/default.conf

# Fix Nginx permissions to run as non-root
RUN mkdir -p /var/lib/nginx/tmp /var/lib/nginx/logs /var/log/nginx /run/nginx \
    && chown -R www-data:www-data /var/lib/nginx /var/log/nginx /run/nginx \
    && sed -i 's/^#*user .*/user www-data;/' /etc/nginx/nginx.conf \
    && ln -sf /dev/stdout /var/log/nginx/access.log \
    && ln -sf /dev/stderr /var/log/nginx/error.log \
    && ln -sf /dev/stderr /var/lib/nginx/logs/error.log

# Set up working directory
WORKDIR {{.OroRootDir}}

# Install OroCommerce and create database dumps for development and testing
RUN --mount=type=cache,target=/root/.composer/cache \
    --mount=type=tmpfs,target=/var/lib/postgresql/data \
    set -e; \
    mkdir -p /opt/oro_backups && chmod 777 /opt/oro_backups; \
    composer create-project oro/commerce-crm-application . {{.OroVersion}} --no-interaction --prefer-dist; \
    ./bin/simple-phpunit --version; \
    apk add --no-cache postgresql postgresql-contrib; \
    mkdir -p /run/postgresql && chown -R postgres:postgres /run/postgresql; \
    chown -R postgres:postgres /var/lib/postgresql/data; \
    su postgres -s /bin/sh -c "initdb -D /var/lib/postgresql/data"; \
    su postgres -s /bin/sh -c "pg_ctl -D /var/lib/postgresql/data -l /tmp/pg.log start"; \
    until su postgres -s /bin/sh -c "pg_isready -q"; do sleep 1; done; \
    su postgres -s /bin/sh -c "psql --command \"CREATE USER oro_db_user WITH SUPERUSER PASSWORD 'oro_db_pass';\"" || { cat /tmp/pg.log; exit 1; }; \
    su postgres -s /bin/sh -c "createdb -O oro_db_user oro_db" || { cat /tmp/pg.log; exit 1; }; \
    su postgres -s /bin/sh -c "createdb -O oro_db_user oro_db_test" || { cat /tmp/pg.log; exit 1; }; \
    export ORO_DB_URL=postgresql://oro_db_user:oro_db_pass@127.0.0.1/oro_db; \
    export ORO_MQ_DSN=dbal: ORO_SEARCH_ENGINE_DSN=orm: ORO_SESSION_DSN=native:; \
    php bin/console oro:install --no-interaction --application-url={{with index .Domains 0}}http{{if .Ssl}}s{{end}}://{{.Host}}/{{end}} --organization-name=OroInc --user-name=admin --user-email=admin@example.com --user-firstname=John --user-lastname=Doe --user-password=admin --sample-data=y; \
    PGPASSWORD=oro_db_pass pg_dump -h 127.0.0.1 -U oro_db_user oro_db | gzip > /opt/oro_backups/oro_db_dev.sql.gz; \
    export ORO_ENV=test ORO_DB_URL=postgresql://oro_db_user:oro_db_pass@127.0.0.1/oro_db_test; \
    php bin/console oro:install --no-interaction; \
    PGPASSWORD=oro_db_pass pg_dump -h 127.0.0.1 -U oro_db_user oro_db_test | gzip > /opt/oro_backups/oro_db_test.sql.gz; \
    ls -lh /opt/oro_backups/; \
    [ -s /opt/oro_backups/oro_db_dev.sql.gz ] || { echo "Error: oro_db_dev.sql.gz is missing or empty"; exit 1; }; \
    [ -s /opt/oro_backups/oro_db_test.sql.gz ] || { echo "Error: oro_db_test.sql.gz is missing or empty"; exit 1; }; \
    su postgres -s /bin/sh -c "pg_ctl -D /var/lib/postgresql/data stop"; \
    rm -f /tmp/pg.log; \
    apk del postgresql postgresql-contrib; \
    chown -R www-data:www-data .; \
    ln -sf /usr/bin/jpegoptim /usr/local/bin/jpegoptim; \
    ln -sf /usr/bin/pngquant /usr/local/bin/pngquant; \
    ln -sf /usr/bin/optipng /usr/local/bin/optipng; \
    rm -f /usr/local/etc/php/conf.d/*phar* || true

# Create required directories for nginx and tools
RUN mkdir -p /opt/oro-nginx-tool /opt/oro-nginx/etc/sites-available \
    && echo '{"properties": {"sites": {"propertyNames": {"pattern": ""}}}}' > /opt/oro-nginx-tool/variables.schema.json

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Install php-fpm-healthcheck
RUN wget -O /usr/local/bin/php-fpm-healthcheck https://raw.githubusercontent.com/renatomefi/php-fpm-healthcheck/master/php-fpm-healthcheck \
    && chmod +x /usr/local/bin/php-fpm-healthcheck \
    && apk add --no-cache fcgi

# Expose ports
EXPOSE {{.NginxHTTPPort}}
{{if .HasSsl}}EXPOSE {{.NginxHTTPSPort}}{{end}}

# Entrypoint and default command
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["nginx"]
