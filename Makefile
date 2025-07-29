# Makefile for common Docker development tasks

up:
	docker-compose up --build

down:
	docker-compose down

migrate:
	docker-compose exec web python manage.py migrate

createsuperuser:
	docker-compose exec web python manage.py createsuperuser

collectstatic:
	docker-compose exec web python manage.py collectstatic --noinput

shell:
	docker-compose exec web python manage.py shell

logs:
	docker-compose logs -f

ps:
	docker-compose ps
