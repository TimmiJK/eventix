include .env
export

docker-up:
	sudo docker compose --env-file .env -f deployments/docker-compose.yml up -d

docker-check:
	sudo docker exec -it ${DB_NAME}-postgres psql -U ${DB_USER} -d ${DB_NAME}

docker-stop:
	sudo docker stop ${DB_NAME}-postgres

docker-down:
	sudo docker compose --env-file .env -f deployments/docker-compose.yml down -v

docker-logs:
	sudo docker compose --env-file .env -f deployments/docker-compose.yml logs -f

proto-auth:
	protoc \
		--proto_path=proto/auth \
		--go_out=proto/auth/pb \
		--go_opt=paths=source_relative \
		--go-grpc_out=proto/auth/pb \
		--go-grpc_opt=paths=source_relative \
		proto/auth/auth.proto

migrate-up:
	migrate -path migrations -database "postgresql://${DB_NAME}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgresql://${DB_NAME}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" down 1

migrate-up-test:
	migrate -path migrations -database "postgresql://${DB_NAME_TEST}:${DB_PASSWORD_TEST}@${DB_HOST_TEST}:${DB_PORT_TEST}/${DB_NAME_TEST}?sslmode=disable" up

migrate-down-test:
	migrate -path migrations -database "postgresql://${DB_NAME_TEST}:${DB_PASSWORD_TEST}@${DB_HOST_TEST}:${DB_PORT_TEST}/${DB_NAME_TEST}?sslmode=disable" down 1

migrate-create:
	@test -n "$(NAME)" || (echo "Ошибка: укажи NAME=название" && exit 1)
	migrate create -ext sql -dir migrations -seq $(NAME)

test-covarage:
	cd services/auth/internal/service && \
	go test -v -race -coverprofile=cover.out && go tool cover -html=cover.out -o cover.html

run-%:
	go run services/$*/cmd/main.go