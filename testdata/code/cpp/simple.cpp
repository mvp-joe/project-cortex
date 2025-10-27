#include <string>
#include <vector>
#include <memory>

const int MAX_CONNECTIONS = 100;
const std::string DEFAULT_HOST = "localhost";

static int global_counter = 0;

struct Point {
    double x;
    double y;

    Point(double x, double y) : x(x), y(y) {}
};

class User {
private:
    std::string id;
    std::string name;
    std::string email;

public:
    User(const std::string& id, const std::string& name, const std::string& email)
        : id(id), name(name), email(email) {}

    std::string getId() const { return id; }
    std::string getName() const { return name; }
    std::string getEmail() const { return email; }

    bool validate() const {
        return email.find('@') != std::string::npos;
    }
};

template<typename T>
class Repository {
private:
    std::vector<T> items;

public:
    Repository() {}

    void add(const T& item) {
        items.push_back(item);
    }

    size_t size() const {
        return items.size();
    }

    const T* get(size_t index) const {
        if (index < items.size()) {
            return &items[index];
        }
        return nullptr;
    }
};

typedef Repository<User> UserRepository;

std::unique_ptr<User> createUser(const std::string& id, const std::string& name, const std::string& email) {
    return std::make_unique<User>(id, name, email);
}
