# Dockerfile template for OroCommerce development
ARG PHP_VERSION={{.PHPVersion}}
ARG NODE_VERSION={{.NodeVersion}}
ARG NPM_VERSION={{.NpmVersion}}
ARG MEMORY_LIMIT={{.MemoryLimit}}

# Get Node from official image
FROM node:${NODE_VERSION}-alpine AS node_image

# Stage 1: Build PHP extensions
FROM php:${PHP_VERSION}-fpm-alpine AS builder

ARG PHP_VERSION

# Build-time dependencies
RUN apk add --no-cache \
    $PHPIZE_DEPS \
    postgresql-dev \
    freetype-dev \
    libjpeg-turbo-dev \
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
    linux-headers

# Install PHP extensions
{{ if or (eq .PHPVersion "8.4") (eq .PHPVersion "8.5") -}}
ADD https://github.com/mlocati/docker-php-extension-installer/releases/latest/download/install-php-extensions /usr/local/bin/
RUN chmod +x /usr/local/bin/install-php-extensions && install-php-extensions imap opcache \
    && docker-php-source extract \
    && docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
    && docker-php-ext-install -j$(nproc) gd pdo_pgsql intl zip bcmath soap xml pcntl tidy \
    && docker-php-source delete
{{- else -}}
RUN docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
    && docker-php-ext-configure imap --with-kerberos --with-imap-ssl \
    && docker-php-ext-install -j$(nproc) gd pdo_pgsql intl zip opcache bcmath soap xml pcntl tidy imap
{{- end }}

# Install Xdebug and PCOV
RUN if [ "{{.Type}}" != "demo" ]; then \
        pecl install xdebug pcov && docker-php-ext-enable pcov xdebug; \
    else \
        pecl install pcov && docker-php-ext-enable pcov; \
    fi

# Stage 2: Final runtime image
FROM php:${PHP_VERSION}-fpm-alpine

ARG PHP_VERSION
ARG NODE_VERSION
ARG NPM_VERSION
ARG MEMORY_LIMIT

# Runtime system dependencies
RUN apk add --no-cache \
    nginx \
    jq \
    chromium \
    bash \
    curl \
    chromium-chromedriver \
    postgresql-client \
    libpng \
    libjpeg-turbo \
    freetype \
    libwebp \
    libzip \
    icu-libs \
    libxml2 \
    tidyhtml-libs \
    c-client \
    krb5-libs \
    openssl \
    jpegoptim \
    pngquant \
    optipng \
    libjpeg-turbo-utils \
    git \
    unzip \
    libstdc++ \
    libgcc \
    ca-certificates \
    fcgi

# Install Composer
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
ENV COMPOSER_ALLOW_SUPERUSER=1

# Install Node.js from official image
COPY --from=node_image /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node_image /usr/local/bin/node /usr/local/bin/node
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && npm install -g npm@${NPM_VERSION} \
    && mkdir -p /.npm /.composer && chmod -R 777 /.npm /.composer

ENV NPM_CONFIG_CACHE=/.npm
ENV COMPOSER_HOME=/.composer
ENV npm_config_update_notifier=false

# Copy PHP extensions from builder
COPY --from=builder /usr/local/lib/php/extensions /usr/local/lib/php/extensions
COPY --from=builder /usr/local/etc/php/conf.d /usr/local/etc/php/conf.d

# Configure PHP, Xdebug and FPM
RUN { \
        if [ "{{.Type}}" != "demo" ]; then \
            echo "zend_extension=xdebug"; \
            echo "xdebug.mode=develop,debug"; \
            echo "xdebug.max_nesting_level=500"; \
            echo "xdebug.client_host=host.docker.internal"; \
            echo "xdebug.client_port=9003"; \
            echo "xdebug.discover_client_host=no"; \
            echo "xdebug.start_with_request=yes"; \
            echo "xdebug.idekey=PHPSTORM"; \
            echo "xdebug.log=/tmp/xdebug.log"; \
            echo "xdebug.connect_timeout_ms=200"; \
        fi \
    } > /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini \
    && if [ "{{.Type}}" != "demo" ]; then mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled; fi \
    && { \
        echo "memory_limit=${MEMORY_LIMIT}"; \
        echo 'upload_max_filesize=100M'; \
        echo 'post_max_size=100M'; \
        echo 'max_execution_time=600'; \
        echo 'date.timezone=UTC'; \
        echo 'short_open_tag=off'; \
        echo 'intl.error_level=0'; \
        echo "opcache.enable={{if eq .Type "demo"}}1{{else}}0{{end}}"; \
        echo "opcache.enable_cli={{if eq .Type "demo"}}1{{else}}0{{end}}"; \
        echo 'opcache.memory_consumption=256'; \
        echo 'opcache.interned_strings_buffer=16'; \
        echo 'opcache.max_accelerated_files=20000'; \
        echo 'opcache.validate_timestamps={{if eq .Type "demo"}}0{{else}}1{{end}}'; \
        echo 'opcache.revalidate_freq=0'; \
        echo 'opcache.fast_shutdown=1'; \
    } > /usr/local/etc/php/conf.d/oro-custom.ini \
    && { \
        echo '[www]'; \
        echo 'pm = dynamic'; \
        echo 'pm.max_children = 20'; \
        echo 'pm.start_servers = 5'; \
        echo 'pm.min_spare_servers = 3'; \
        echo 'pm.max_spare_servers = 10'; \
    } > /usr/local/etc/php-fpm.d/z-oro-fpm.conf \
    && chmod -R 777 /usr/local/etc/php/conf.d

# Configure Nginx
COPY nginx.conf /etc/nginx/http.d/default.conf
RUN mkdir -p /var/lib/nginx/tmp /var/lib/nginx/logs /var/log/nginx /run/nginx \
    && chown -R root:root /var/lib/nginx /var/log/nginx /run/nginx \
    && sed -i 's/^#*user .*/user root;/' /etc/nginx/nginx.conf \
    && ln -sf /dev/stdout /var/log/nginx/access.log \
    && ln -sf /dev/stderr /var/log/nginx/error.log \
    && ln -sf /dev/stderr /var/lib/nginx/logs/error.log

# Set up working directory
WORKDIR {{.OroRootDir}}

# Final permissions and symlinks
RUN ln -sf /usr/bin/jpegoptim /usr/local/bin/jpegoptim; \
    ln -sf /usr/bin/pngquant /usr/local/bin/pngquant; \
    ln -sf /usr/bin/optipng /usr/local/bin/optipng; \
    rm -f /usr/local/etc/php/conf.d/*phar* || true;

# Create required directories for nginx and tools
RUN mkdir -p /opt/oro-nginx-tool /opt/oro-nginx/etc/sites-available \
    && echo '{"properties": {"sites": {"propertyNames": {"pattern": ""}}}}' > /opt/oro-nginx-tool/variables.schema.json

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Install php-fpm-healthcheck
RUN wget -O /usr/local/bin/php-fpm-healthcheck https://raw.githubusercontent.com/renatomefi/php-fpm-healthcheck/master/php-fpm-healthcheck \
    && chmod +x /usr/local/bin/php-fpm-healthcheck

# Expose ports
EXPOSE 80 443

# Entrypoint and default command
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["nginx"]
