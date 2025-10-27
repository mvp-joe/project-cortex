package com.example.app;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

public class UserService {
    private static final String API_KEY = "test-api-key";
    private static final int MAX_RETRIES = 3;

    private static int globalCounter = 0;

    private List<User> users;

    public UserService() {
        this.users = new ArrayList<>();
    }

    public void addUser(User user) {
        if (user.validate()) {
            users.add(user);
        }
    }

    public Optional<User> findById(String id) {
        return users.stream()
            .filter(u -> u.getId().equals(id))
            .findFirst();
    }

    public int getUserCount() {
        return users.size();
    }
}

class User {
    private final String id;
    private final String name;
    private final String email;

    public User(String id, String name, String email) {
        this.id = id;
        this.name = name;
        this.email = email;
    }

    public String getId() {
        return id;
    }

    public String getName() {
        return name;
    }

    public String getEmail() {
        return email;
    }

    public boolean validate() {
        return email != null && email.contains("@");
    }
}

interface Repository<T> {
    void add(T item);
    Optional<T> findById(String id);
    List<T> findAll();
}

enum UserStatus {
    ACTIVE,
    INACTIVE,
    PENDING
}
