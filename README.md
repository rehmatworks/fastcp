![fastcp-control-panel](https://fastcp.org/images/prototype.png "FastCP Control Panel")

# FastCP
FastCP is an open source control panel for Ubuntu servers. You can use FastCP to deploy and manage multiple PHP / WordPress websites on a single server. ServerPilot's simplicity and powerful features are the inspiration behind FastCP's development. Moreover, I have developed this control panel as the final project of my CS50 online course.

## Features
* Host multiple websites on a single server
* Create multiple SSH users
* Sub users can manage their websites
* Limit on websites and databases for sub users
* Auto WordPress deploy
* Fully isolated user data using ACLs
* NGINX reverse proxy on Apache for performance + htaccess support
* Multiple PHP versions support. Change PHP version per website with a single click
* Auto SSLs from Let's Encrypt with auto renewal

## Requirements
FastCP only supports the latest LTS versions of Ubuntu starting 20.04. Please beware although it will run on non-LTS releases too, but we have imposed a strict requirement of LTS releases only. At the moment, FastCP supports the following Ubuntu releases:

* Ubuntu 20.04 LTS

## How to Install?
You can visit [https://fastcp.org](https://fastcp.org) to install FastCP on your server or you can execute the following command as root user on your Ubuntu server:

```bash
cd /home && sudo curl -o latest https://fastcp.org/latest.sh && sudo bash latest
```

## How to Update?
To update FastCP to latest version, execute this command as root user:
```bash
cd ~/ && sudo fastcp-updater
```

## Docker Setup

You can run FastCP locally or in production using Docker and Docker Compose. This setup supports PostgreSQL, MySQL, or SQLite (default).

### 1. Copy and configure environment variables

Copy the example environment file and edit as needed:

```bash
cp .env.example .env
# Edit .env to set your DB engine and credentials
```

### 2. Build and start the containers

```bash
docker-compose up --build
```

- By default, PostgreSQL is used. To use MySQL, uncomment the MySQL service in `docker-compose.yml` and set `DJANGO_DB_ENGINE=mysql` in your `.env`.
- For SQLite, set `DJANGO_DB_ENGINE=sqlite` and comment out DB services.

### 3. Run migrations and create a superuser

In a new terminal:

```bash
docker-compose exec web python manage.py migrate
docker-compose exec web python manage.py createsuperuser
```

### 4. Collect static files (for production)

```bash
docker-compose exec web python manage.py collectstatic
```

---