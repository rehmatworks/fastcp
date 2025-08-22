FROM phpmyadmin:5.2.1

# Install system libs required by gd and zip
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends libpng16-16 libzip4 \
    && rm -rf /var/lib/apt/lists/*

# Add a small Apache config to set ServerName and disable server-status
COPY phpmyadmin.conf /etc/apache2/conf-available/phpmyadmin.conf
RUN a2enconf phpmyadmin.conf || true

# Keep original entrypoint
USER www-data
