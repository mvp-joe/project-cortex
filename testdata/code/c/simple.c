#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define MAX_USERS 100
#define BUFFER_SIZE 256

const int DEFAULT_PORT = 8080;
static int connection_count = 0;

// Named struct for testing
struct User {
    int id;
    char name[BUFFER_SIZE];
    char email[BUFFER_SIZE];
};

// Named struct for testing
struct UserRepository {
    struct User users[MAX_USERS];
    int count;
};

// Typedef aliases
typedef struct User User;
typedef struct UserRepository UserRepository;

UserRepository* create_repository() {
    UserRepository* repo = (UserRepository*)malloc(sizeof(UserRepository));
    repo->count = 0;
    return repo;
}

int add_user(UserRepository* repo, User user) {
    if (repo->count >= MAX_USERS) {
        return -1;
    }
    repo->users[repo->count] = user;
    repo->count++;
    return 0;
}

User* find_user(UserRepository* repo, int id) {
    for (int i = 0; i < repo->count; i++) {
        if (repo->users[i].id == id) {
            return &repo->users[i];
        }
    }
    return NULL;
}

void free_repository(UserRepository* repo) {
    free(repo);
}
