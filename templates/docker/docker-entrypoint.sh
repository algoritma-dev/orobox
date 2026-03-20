#!/bin/bash
set -e

# Load environment variables from .env if it exists
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

# Ensure OroRootDir is set
ORO_ROOT_DIR={{.OroRootDir}}
cd ${ORO_ROOT_DIR}

# Enable/Disable Xdebug
if [ "$ORO_XDEBUG_ENABLED" = "true" ] || [ "$ORO_XDEBUG_ENABLED" = "1" ]; then
    if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled ]; then
        mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini
    fi
else
    if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini ]; then
        mv /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled
    fi
fi

case "$1" in
    nginx)
        if [ -n "$ORO_USER_RUNTIME" ]; then
            # Replace : with space for Nginx user directive (user [user] [group])
            NGINX_USER=$(echo $ORO_USER_RUNTIME | tr ':' ' ')
            sed -i "s/^#*user .*/user $NGINX_USER;/" /etc/nginx/nginx.conf
            # Ensure Nginx can write to its directories as the specified user
            chown -R $ORO_USER_RUNTIME /var/lib/nginx /var/log/nginx /run/nginx || true
        fi
        exec nginx -g 'daemon off;'
        ;;
    php-fpm)
        exec php-fpm
        ;;
    websocket)
        exec php bin/console gos:websocket:server --env=$ORO_ENV
        ;;
    consumer)
        exec php bin/console oro:message-queue:consume --env=$ORO_ENV
        ;;
    cron)
        while true; do
            php bin/console oro:cron --env=$ORO_ENV
            sleep 60
        done
        ;;
    install)
        shift
        # Wait for DB to be ready
        if [ -n "$ORO_DB_HOST" ]; then
            echo "Waiting for database ${ORO_DB_HOST}:${ORO_DB_PORT:-5432}..."
            until pg_isready -h ${ORO_DB_HOST} -p ${ORO_DB_PORT:-5432} -U ${ORO_DB_USER:-oro_db_user} > /dev/null 2>&1; do
                sleep 1
            done
            echo "Database is up!"
        fi

        # Build install command options
        INSTALL_OPTS=()
        # Use ORO_APP_URL if defined, otherwise build it from protocol and domain
        if [ -n "$ORO_APP_URL" ]; then
            INSTALL_OPTS+=( "--application-url=${ORO_APP_URL}" )
        elif [ -n "$ORO_APP_PROTOCOL" ] && [ -n "$ORO_APP_DOMAIN" ]; then
            INSTALL_OPTS+=( "--application-url=${ORO_APP_PROTOCOL}://${ORO_APP_DOMAIN}" )
        fi

        [ -n "$ORO_ORGANIZATION_NAME" ] && INSTALL_OPTS+=( "--organization-name=${ORO_ORGANIZATION_NAME}" )
        [ -n "$ORO_USER_NAME" ] && INSTALL_OPTS+=( "--user-name=${ORO_USER_NAME}" )
        [ -n "$ORO_USER_EMAIL" ] && INSTALL_OPTS+=( "--user-email=${ORO_USER_EMAIL}" )
        [ -n "$ORO_USER_FIRSTNAME" ] && INSTALL_OPTS+=( "--user-firstname=${ORO_USER_FIRSTNAME}" )
        [ -n "$ORO_USER_LASTNAME" ] && INSTALL_OPTS+=( "--user-lastname=${ORO_USER_LASTNAME}" )
        [ -n "$ORO_USER_PASSWORD" ] && INSTALL_OPTS+=( "--user-password=${ORO_USER_PASSWORD}" )
        [ -n "$ORO_SAMPLE_DATA" ] && INSTALL_OPTS+=( "--sample-data=${ORO_SAMPLE_DATA}" )
        [ -n "$ORO_LANGUAGE" ] && INSTALL_OPTS+=( "--language=${ORO_LANGUAGE}" )
        [ -n "$ORO_FORMATTING_CODE" ] && INSTALL_OPTS+=( "--formatting-code=${ORO_FORMATTING_CODE}" )
    
        echo "Running: php bin/console oro:install --no-interaction ${INSTALL_OPTS[*]} $ORO_INSTALL_OPTIONS $*"
        php bin/console oro:install --no-interaction "${INSTALL_OPTS[@]}" $ORO_INSTALL_OPTIONS "$@"
        STATUS=$?
        if [ $STATUS -ne 0 ]; then
            echo "Error: Installation failed with status $STATUS"
            # Attempt to show any logs if available
            [ -f var/logs/prod.log ] && tail -n 50 var/logs/prod.log
            [ -f var/logs/dev.log ] && tail -n 50 var/logs/dev.log
            exit $STATUS
        fi
        exit 0
        ;;
    restore)
        # Wait for DB to be ready
        if [ -n "$ORO_DB_HOST" ]; then
            echo "Waiting for database ${ORO_DB_HOST}:${ORO_DB_PORT:-5432}..."
            until pg_isready -h ${ORO_DB_HOST} -p ${ORO_DB_PORT:-5432} -U ${ORO_DB_USER:-oro_db_user} > /dev/null 2>&1; do
                sleep 1
            done
            echo "Database is up!"
        fi

        check_and_restore() {
            local db_name="$1"
            local backup_file="$2"
            echo "Checking database $db_name..."
            if [ -f "$backup_file" ]; then
                local table_count=$(PGPASSWORD=$ORO_DB_ROOT_PASSWORD psql -h $ORO_DB_HOST -p ${ORO_DB_PORT:-5432} -U $ORO_DB_ROOT_USER -d $db_name -t -c "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';" | tr -d '[:space:]' || echo "0")
                echo "Found $table_count tables in $db_name"
                if [ "$table_count" = "0" ] || [ -z "$table_count" ]; then
                    echo "Restoring $db_name from $backup_file..."
                    if gunzip -c "$backup_file" | PGPASSWORD=$ORO_DB_ROOT_PASSWORD psql -h $ORO_DB_HOST -p ${ORO_DB_PORT:-5432} -U $ORO_DB_ROOT_USER -d $db_name > /tmp/restore_$db_name.log 2>&1; then
                        echo "Restore of $db_name completed."

                        echo "Ensuring schema is up to date (this may take a few minutes)..."
                        php bin/console oro:platform:update --force --no-interaction --env=${ORO_ENV:-prod}
                    else
                        echo "Error: Restore of $db_name failed. See /tmp/restore_$db_name.log"
                        cat /tmp/restore_$db_name.log
                        exit 1
                    fi
                else
                    echo "Database $db_name already contains data. Skipping restore."
                fi
            else
                echo "Warning: Backup file $backup_file not found. Skipping restore for $db_name."
            fi
        }

        [ -n "$ORO_DB_NAME" ] && check_and_restore "$ORO_DB_NAME" "/opt/oro_backups/oro_db_dev.sql.gz"
        [ -n "$ORO_DB_NAME_TEST" ] && check_and_restore "$ORO_DB_NAME_TEST" "/opt/oro_backups/oro_db_test.sql.gz"

        exit 0
        ;;
    nginx-init)
        echo "Nginx init..."
        mkdir -p /opt/oro-nginx/etc/sites-available/
        cp /etc/nginx/http.d/default.conf /opt/oro-nginx/etc/sites-available/oro.conf
        exit 0
        ;;
    *)
        exec "$@"
        ;;
esac
